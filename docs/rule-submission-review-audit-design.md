# Rule Submission Maintainer Review and Audit Design

This document freezes the first bounded maintainer-review contract for `RuleSubmission` after controlled submission transport, replay safety, PostgreSQL concurrency protection, and race-detection gates are already in place.

It is a **design-only** step for Issue #35. It does not add review mutation code, reviewer HTTP routes, OAuth/RBAC, frontend review buttons, publication to `RiskRule`, bots, or AI review.

## Decision summary

1. The first review state machine contains exactly three states: `pending`, `approved`, and `rejected`.
2. The only allowed transitions in this slice are `pending -> approved` and `pending -> rejected`.
3. `approved` and `rejected` are terminal for the first implementation. There is no reopen/change-requested transition yet.
4. Approval must re-run the current `ValidateDraft` rules against the **stored submission snapshot** before the transition can commit.
5. Rejection does not require the draft to remain currently valid; maintainers must still be able to reject stale or invalid pending content.
6. Approval means “human review accepted this proposal.” It does **not** create, enable, or mutate a `RiskRule`.
7. Every committed terminal transition must atomically append exactly one `RuleSubmissionReviewEvent`.
8. Review events are append-only by application/database contract, but this is not a cryptographically tamper-proof or DBA-proof audit system.
9. The first controlled review transport must use a **separate review credential** from the existing submission write credential.
10. Because the repository has no authenticated reviewer identity model yet, audit attribution uses a server-trusted actor label and must not be presented as OAuth/RBAC-backed personal identity.
11. Review reason is mandatory for both approval and rejection, trimmed, non-empty, and bounded to 2,000 UTF-8 bytes.
12. Concurrent reviewers are resolved by an atomic database state transition; exactly one terminal decision may commit.
13. Status mutation and review-event insertion occur in the same database transaction. Either both commit or neither does.
14. An exact retry of the same already-committed review command may return the existing event as a replay; a different second decision/reason/actor is a conflict.
15. Existing pending-scoped submission idempotency remains unchanged after review: once a submission leaves `pending`, the digest is no longer occupied by the pending unique index.
16. Publication is a separate future operation and must revalidate again immediately before creating a `RiskRule`.

## Current boundary

Today `RuleSubmission` has a current `status`, immutable draft-snapshot fields by service convention, and a server-owned `draft_digest`. Exact replay looks up only `pending` rows. The repository does not currently have reviewer users, reviewer roles, OAuth, or a review-specific authorization credential.

The next implementation must extend this model without pretending those missing identity capabilities already exist.

## State machine

```text
                         approve + current validation passes
                    +------------------------------------------+
                    |                                          v
                +---------+                                +----------+
                | pending |                                | approved |
                +---------+                                +----------+
                    |
                    | reject
                    v
                +----------+
                | rejected |
                +----------+
```

No other transition is valid in the first implementation.

### `pending`

A non-executable community/controlled proposal awaiting human review.

### `approved`

A maintainer accepted the proposal for a later publication step. It is still a `RuleSubmission`, not an active rule.

`approved` must **not**:

- create a `RiskRule`,
- enable a rule in the analysis engine,
- claim publication succeeded,
- imply the proposal remains publishable forever.

### `rejected`

A maintainer rejected the proposal in its current form.

A changed proposal should be submitted as a new `RuleSubmission`. The first implementation does not edit a rejected submission back into `pending`.

## Why there is no `change_requested` state yet

A richer review workflow may eventually need `change_requested`, reopen, superseded, or withdrawn states. Adding them now would force versioning/edit semantics that the project does not yet have.

For the current bounded workflow:

- a pending snapshot is immutable by service contract,
- a maintainer either accepts or rejects that exact snapshot,
- a contributor revision is a new submission.

This keeps each review decision attached to one immutable proposal snapshot.

## Approval-time revalidation

Approval is a new trust decision and must use current validation dependencies.

Before `pending -> approved`, reconstruct a `DraftRequest` from the **stored `RuleSubmission` snapshot** and run current `ValidateDraft` inside the review transaction context.

Approval must fail with **zero state/event writes** when current validation fails, including examples such as:

- the category no longer exists,
- a `RiskRule` with the same code now exists,
- a rule type/severity is no longer supported,
- a regex is no longer accepted by current validation behavior,
- weight or required-field rules changed.

Do not validate a fresh client-supplied copy of the rule. The object under review is the stored snapshot.

### Rejection intentionally does not revalidate

A stale, malformed-by-new-policy, or obsolete pending proposal still needs to be rejectable. Requiring current validation for rejection could trap bad rows permanently in `pending`.

Rejection only requires:

- the submission exists,
- its current status is `pending`,
- the review command is authorized by the future controlled review transport,
- actor label and reason satisfy their bounded contract.

## Publication remains separate

Approval and publication are deliberately decoupled.

A later publication operation should conceptually be:

```text
approved submission
-> current publication authorization
-> revalidate stored snapshot again
-> transactionally create RiskRule
-> record publication audit
```

Revalidation is required again at publication time because mutable dependencies may change between review approval and publication.

The review implementation must not call the current `RiskRule` create handler/service as a side effect.

## Review event model

The next implementation should add an audit entity conceptually equivalent to:

```go
type RuleSubmissionReviewEvent struct {
    ID             uint      `gorm:"primaryKey"`
    SubmissionID   uint      `gorm:"not null;uniqueIndex"`
    Decision       string    `gorm:"size:32;not null"`
    FromStatus     string    `gorm:"size:32;not null"`
    ToStatus       string    `gorm:"size:32;not null"`
    Reason         string    `gorm:"not null"`
    ActorKind      string    `gorm:"size:40;not null"`
    ActorLabel     string    `gorm:"size:120;not null"`
    DraftDigest    string    `gorm:"size:64;not null"`
    CreatedAt      time.Time `gorm:"index"`
}
```

Exact field tags may change to fit GORM/PostgreSQL/SQLite behavior, but the semantics are frozen below.

### `SubmissionID`

References the reviewed submission.

The implementation should create a real foreign-key relationship with `ON DELETE RESTRICT` (or the closest GORM-equivalent that produces the same invariant) so a reviewed submission cannot be deleted while its audit event exists.

The event table uses a unique database invariant on `submission_id` in the first slice because one submission can have exactly one terminal review transition.

If a future workflow introduces reopen/change-requested/multiple review rounds, that migration must deliberately replace this first-version uniqueness rule rather than silently weakening it.

### `Decision`

Allowed first-version values:

- `approved`
- `rejected`

Do not introduce a free-form action string.

### `FromStatus` / `ToStatus`

For the first implementation:

- `from_status` is always `pending`,
- `to_status` equals the decision (`approved` or `rejected`).

Storing both makes the audit record explicit and easier to evolve when more transitions exist later.

### `Reason`

A human review explanation is required for both approval and rejection.

Rules:

- trim leading/trailing whitespace,
- reject an empty result,
- maximum 2,000 UTF-8 bytes,
- do not automatically inject validation output as the human reason,
- do not log the raw reason by default,
- contributors/maintainers must not paste victim PII, credentials, bank-card data, private chat dumps, tokens, or live malicious URLs into review reasons.

The reason is audit context, not an evidence-blob channel.

### Actor attribution without fake identity

The repository currently has no authenticated reviewer identity model. Therefore the first controlled implementation must distinguish between **attribution label** and **verified personal identity**.

Freeze these fields:

- `actor_kind = "controlled_maintainer"`
- `actor_label = <server-trusted configured label>`

The actor label:

- is provided by trusted server configuration/middleware, not by the review request JSON,
- is trimmed, required, and bounded to 120 bytes,
- may identify an operational credential or maintainer console context,
- must not be described in UI/API docs as proof of a specific person's authenticated identity.

When OAuth/RBAC exists later, add a new actor kind/reference model instead of retroactively pretending old labels were authenticated users.

### `DraftDigest`

Every review event stores the deterministic v1 digest of the exact stored draft snapshot reviewed.

For normal new rows this should equal `RuleSubmission.DraftDigest`.

For a legacy pending row whose `DraftDigest` is NULL because it was a later pre-idempotency duplicate, compute the digest from the stored snapshot at review time **without mutating the legacy row solely for audit convenience**.

This binds the decision to a deterministic snapshot fingerprint while preserving the existing legacy migration contract.

The review digest remains internal and is not a secret, signature, proof of authorship, or tamper-proof checksum chain.

## Append-only honesty boundary

`RuleSubmissionReviewEvent` is append-only in the first implementation by these mechanisms:

- no update service,
- no delete service,
- no HTTP update/delete routes,
- no `UpdatedAt` field,
- unique one-terminal-event-per-submission invariant,
- foreign-key protection against deleting the reviewed submission.

This provides a strong normal-application audit contract.

It does **not** protect against a database owner intentionally editing rows directly. The project must not market this as immutable ledger, WORM storage, cryptographic non-repudiation, or tamper-evident audit history.

Those properties require a separate future design.

## Review authorization separation

The existing `RULE_SUBMISSION_WRITE_TOKEN` authorizes controlled **submission creation**. It must not automatically authorize review decisions.

Before any review mutation HTTP route is exposed, add a separate default-off review gate such as:

```text
RULE_SUBMISSION_REVIEWS_ENABLED=false
RULE_SUBMISSION_REVIEW_TOKEN=<independent secret>
RULE_SUBMISSION_REVIEW_ACTOR_LABEL=<non-secret trusted label>
```

Requirements:

- the review token must be independent from `RULE_SUBMISSION_WRITE_TOKEN`,
- configuration should fail closed when reviews are enabled but token/actor label is invalid,
- review actor label is server-owned and never accepted from client JSON,
- token must never be logged,
- a future true identity system should replace this controlled credential model rather than layering public review access on top of a shared token.

The next **review persistence implementation** may remain internal/service-only and defer HTTP transport until these controls are implemented and tested.

## Atomic transition contract

Status mutation and audit append must be one transaction.

Conceptual approval flow:

1. Begin transaction.
2. Load the stored submission snapshot.
3. If it does not exist: return not found, zero writes.
4. If already terminal: evaluate exact review-command replay/conflict semantics described below.
5. Revalidate the stored snapshot using current `ValidateDraft`.
6. If validation fails: rollback/return validation conflict, zero writes.
7. Atomically transition only if status is still `pending`.
8. Insert exactly one review event in the same transaction.
9. If event insertion fails: rollback the status transition.
10. Commit.

Conceptual rejection flow is the same except step 5 is omitted.

### Database compare-and-set is the concurrency authority

Do not implement review correctness with an in-memory mutex.

The transition must ultimately depend on a database predicate equivalent to:

```sql
UPDATE rule_submissions
SET status = :target
WHERE id = :id AND status = 'pending';
```

and require exactly one affected row before the event is committed.

A row lock may be used as an implementation detail, but correctness must still be valid across multiple backend processes.

## Concurrent review semantics

### Same command repeated concurrently

If many concurrent calls carry the exact same trusted actor label, decision, and normalized reason:

- exactly one transaction creates the terminal event,
- the submission reaches that terminal status once,
- losing callers may resolve the committed event as an **exact review replay**,
- all successful/replay callers must return the same event/submission terminal state,
- no second event may be inserted.

### Conflicting commands

Examples:

- reviewer A approves while reviewer B rejects,
- same actor submits `approved` with two different reasons,
- different actor labels attempt the same decision after one already committed.

Exactly one first terminal decision wins. All non-identical later commands return a conflict and must create zero additional events.

The first implementation does not implement voting, quorum, maker-checker, or multi-review consensus.

## Review-command replay identity

Network retries of a successful review should not create confusing conflicts when the exact command is repeated.

An already-terminal review is an exact command replay only when all of these match the stored event after normalization:

- submission ID,
- decision,
- trimmed reason,
- trusted actor kind,
- trusted actor label.

If they match, return the existing event/state without a write.

If any differ, return conflict.

No client-provided audit/event ID is required in this slice.

## Interaction with submission exact replay

The existing submission digest unique index is intentionally `pending`-scoped. Review must not silently change that design.

When a submission transitions to `approved` or `rejected`:

- its original `draft_digest` remains stored on that historical submission,
- it leaves the pending unique-index predicate,
- a later exact submission of the same draft may create a **new pending submission** if current validation passes.

This allows deliberate reconsideration/resubmission after a terminal review and remains consistent with the existing idempotency design.

Do not add global lifetime digest uniqueness in the review implementation.

## Read/audit contract

The first implementation should provide internal read functions before exposing review UI:

- list review events for a submission ordered by `created_at, id`,
- fetch the terminal event for a submission,
- inspect a submission's current status together with its review event.

Because the first model permits only one terminal event, listing may currently return zero or one item. Preserve ordered-list semantics so future multi-event workflows do not require a conceptual API rewrite.

Read paths must not mutate status, events, or `RiskRule` rows.

## Future HTTP semantics (not implemented by this design)

A later controlled review endpoint may look conceptually like:

```text
POST /api/v1/rule-submissions/{id}/reviews
```

with client body limited to:

```json
{
  "decision": "approved",
  "reason": "Matches the documented fraud pattern and has an acceptable false-positive profile."
}
```

The client must not be allowed to supply:

- actor kind/label,
- current/from/to status,
- draft digest,
- timestamps,
- reviewer user ID,
- publication flags.

Suggested result classes for later transport design:

- success/replay -> `200`
- missing submission -> `404`
- invalid decision/reason -> `400`
- terminal conflicting review -> `409`
- approval current-validation conflict -> `409` (with stable validation error semantics designed before transport implementation)
- authorization failure -> `401`
- review safety dependency unavailable -> fail closed

Do not expose a review mutation route until separate review authorization is present.

## Logging and data safety

Allowed operational logging should be limited to metadata such as:

- route/status/latency,
- submission ID,
- target decision,
- created-versus-replay-versus-conflict outcome,
- actor label only if it is explicitly classified non-secret,
- validation field/error codes.

Do not log by default:

- review token,
- raw submission body,
- rule pattern/free text,
- review reason,
- canonical draft JSON,
- submission draft digest.

## Database integrity expectations

The next implementation must establish and test at least these invariants:

1. one terminal review event per submission in the first schema version,
2. review event references an existing submission,
3. a reviewed submission cannot be deleted while its event exists,
4. event decision and transition are from the frozen allowed set,
5. application service cannot commit terminal status without its event,
6. application service cannot commit event without matching terminal status,
7. event creation failure rolls back the status update,
8. two concurrent terminal decisions cannot both commit.

A global database `CHECK` on `RuleSubmission.status` is **not required in the first implementation** if it would make legacy upgrades unsafe. Service transitions must still use exact constants and compare-and-set predicates. A later explicit migration may add a status check after legacy-state compatibility is audited.

## Implementation acceptance criteria

The next code PR should be complete only if all of the following are true:

1. Status constants exist for `pending`, `approved`, and `rejected`.
2. Only `pending -> approved` and `pending -> rejected` are accepted.
3. Review reason is server-validated after trim, non-empty, and <= 2,000 UTF-8 bytes.
4. Actor kind/label are trusted service inputs, not client-controlled submission fields.
5. Approval reconstructs and validates the stored submission snapshot using current `ValidateDraft`.
6. Approval validation failure produces zero status/event/RiskRule writes.
7. Rejection can still transition a currently invalid/stale pending snapshot.
8. Status transition and event insert are atomic in one transaction.
9. Event insert failure demonstrably rolls back status mutation.
10. Exactly one review event exists per terminal submission in this first version.
11. Event stores a deterministic reviewed-draft digest even for legacy NULL-digest rows.
12. Event has no update/delete application path.
13. FK/restrict behavior prevents deleting a reviewed submission.
14. Exact review-command retry returns the existing event without a new write.
15. Different second review command returns conflict.
16. Concurrent identical reviews converge on one terminal event.
17. Concurrent approve/reject commands result in exactly one winner/event.
18. Approval creates **zero** `RiskRule` rows.
19. Rejection creates **zero** `RiskRule` rows.
20. Existing submission replay, Redis rate limiting, PostgreSQL concurrency, and transport tests remain green.
21. `go test ./...`, targeted `go test -race -count=1`, and `go vet ./...` pass.
22. Frontend install/typecheck/build gates remain green even though no review UI is added.
23. Real PostgreSQL integration coverage validates transaction/concurrency/FK behavior before merge.

## Required test matrix

### Service/state tests

- approve valid pending -> approved + one event,
- reject pending -> rejected + one event,
- approve already approved with exact same command -> replay/no write,
- approve/reject after different terminal command -> conflict/no write,
- invalid decision -> zero writes,
- blank reason -> zero writes,
- oversized reason -> zero writes,
- missing/blank trusted actor label -> zero writes,
- approve creates zero `RiskRule`,
- reject creates zero `RiskRule`.

### Revalidation tests

- category removed after submission -> approval blocked, zero writes,
- `RiskRule` with same code appears after submission -> approval blocked, zero writes,
- the same stale row can still be rejected,
- publication is not invoked by approval.

### Audit/integrity tests

- event captures from/to/decision/reason/actor/digest correctly,
- legacy NULL-digest submission gets event digest computed from stored snapshot without rewriting the row digest,
- reviewed submission delete is rejected by FK,
- there is no event update/delete service,
- injected event-insert failure rolls back terminal status.

### PostgreSQL concurrency tests

Run concurrent review attempts against real PostgreSQL:

- many identical approvals -> one event/one terminal transition; exact retries resolve consistently,
- approve versus reject race -> exactly one terminal state and one event,
- no transaction leaves status/event mismatched,
- zero `RiskRule` side effects,
- race detector remains clean for Go code paths.

SQLite may retain fast behavior tests, but transaction/concurrency/FK correctness must be demonstrated on PostgreSQL in CI.

## Explicit non-goals

The next implementation must not add:

- public reviewer registration,
- OAuth, SSO, or RBAC,
- multiple reviewer quorum,
- maker-checker approval,
- reopen/change-requested/withdrawn states,
- automatic publication to `RiskRule`,
- rule version history,
- contributor reputation,
- attachments/evidence blobs,
- GitHub bots,
- AI-generated approval decisions,
- cryptographic audit chaining or external immutable storage.

## Follow-up sequence

After this design:

1. **Review state + audit persistence implementation** — internal service/database first; no review HTTP mutation route required.
2. **Controlled review transport** — separate review feature flag/token/actor label, bounded JSON, authorization and negative tests.
3. **Maintainer read UI** — inspect pending proposal + audit state; still no automatic publication.
4. **Publication design** — freeze approved-to-RiskRule semantics, revalidation and publication audit.
5. **Publication implementation** — explicit human action only.
6. **Community identity design** — replace controlled shared credentials with real contributor/reviewer identity when abuse/auth boundaries are ready.

This keeps the community workflow explainable and auditable while avoiding premature identity claims or automatic activation of community-submitted anti-fraud rules.
