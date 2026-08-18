# Approved Rule Submission Publication and Provenance Design

This document freezes the first bounded publication contract for turning an already-approved `RuleSubmission` into a `RiskRule`.

It is a **design-only** step for Issue #47. It does not add publication runtime code, HTTP routes, configuration, credentials, Redis limits, frontend UI, contributor/reviewer identity, or rule versioning.

The existing review boundary remains authoritative:

```text
approved != published
```

A review approval means a maintainer accepted one immutable submission snapshot for a later publication decision. Publication is a separate higher-impact operation that can create an executable `RiskRule` and must therefore revalidate and re-establish integrity at publication time.

Progress toward #1.

## Existing frozen dependencies

The next implementation must reuse the current repository contracts rather than invent parallel behavior:

- `RuleSubmission.status` currently uses `pending`, `approved`, and `rejected`.
- `approved` and `rejected` are terminal review states in the first review state machine.
- `ReviewPendingSubmission` is the only review mutation service.
- every terminal review has exactly one `RuleSubmissionReviewEvent` in the first schema version,
- an approved review event binds the review to a deterministic draft digest,
- approval already revalidates the stored submission snapshot,
- approval itself creates/updates/enables zero `RiskRule` rows,
- pending submission replay remains scoped to `status = 'pending'`,
- current rule validation is implemented by `ValidateDraft`,
- `RiskRule.code` is already database-unique,
- `RiskRule` currently supports later update, toggle, and hard-delete operations,
- the repository currently uses GORM `AutoMigrate` rather than a numbered migration framework.

This design must fit those facts instead of silently replacing them.

## Decision summary

1. Only an existing `RuleSubmission` whose current review status is exactly `approved` is publication-eligible.
2. Publication does **not** add a new `published` value to `RuleSubmission.status`. `approved` remains the review result; publication existence is represented by a separate publication event.
3. Publication must require the matching approved `RuleSubmissionReviewEvent` and must verify that its reviewed draft digest still matches the stored submission snapshot.
4. Publication must recompute the stored submission snapshot digest and fail closed on digest/review-integrity disagreement.
5. Publication must run current `ValidateDraft` again immediately before first publication using the stored approved snapshot, not client-supplied rule fields.
6. The created `RiskRule` copies the stored approved submission fields exactly, including its stored `Enabled` value. Publication must not silently force enable/disable semantics.
7. A publication-created `RiskRule` gets a server-owned nullable `source_submission_id` provenance field; existing/manual/seed rules keep this field `NULL`.
8. `source_submission_id` is not client-bindable and is set only by the publication service.
9. First publication atomically creates exactly one `RiskRule` and exactly one `RuleSubmissionPublicationEvent` in the same database transaction.
10. The first publication event is append-only by normal application contract and permanently records the approved source submission, the exact approval event, the published rule identifier/code, trusted publisher attribution, and draft digest.
11. The publication event deliberately does **not** use a foreign key to `RiskRule` in the first slice because the current rule lifecycle permits hard deletion; publication audit must survive later rule deletion.
12. The publication event does use restrictive foreign keys to its source submission and source review event so the approval provenance cannot be deleted through normal relational cascades while publication history exists.
13. One approved submission may have at most one publication event in the first schema version.
14. One review event may authorize at most one publication event in the first schema version.
15. An exact retry by the same trusted publisher actor may replay the original publication result; a different trusted publisher actor is a conflict and must not create another rule/event.
16. Same-submission publication concurrency is resolved by database uniqueness/transaction state, not an in-memory mutex.
17. Different approved submissions that race to publish the same `RiskRule.code` are resolved by the existing database unique constraint; exactly one rule may win.
18. If a rule was published and later hard-deleted, retrying publication must **not** recreate it automatically. The historical publication event remains authoritative evidence that publication already occurred.
19. Later `RiskRule` update/toggle/delete operations are outside this first publication audit. Publication provenance describes the initial published snapshot, not the rule's entire future lifecycle.
20. A future publication HTTP transport must be default-off and use a publication credential independent from both submission-write and review credentials.

## State model: review status and publication state are separate

Do not extend the existing review state machine with a `published` review status.

The first model is two-dimensional:

```text
Review state                     Publication state
------------                     -----------------
pending                          not published
rejected                         not published
approved + no publication event  approved / not published
approved + publication event     approved / published historically
```

This avoids overloading one status column with two different trust decisions.

### Why `approved` remains `approved`

Review answers:

> Did a maintainer accept this exact proposal snapshot?

Publication answers:

> Did an independently authorized publication operation create a `RiskRule` from that approved snapshot?

The answers may occur at different times and must remain independently auditable.

### Publication event is authoritative

For the first version, publication existence is derived from `RuleSubmissionPublicationEvent`, not from a mutable boolean on `RuleSubmission`.

Do not add:

- `RuleSubmission.status = published`,
- `RuleSubmission.published = true`,
- a client-controlled publication flag,
- an implicit rule lookup by matching code and assuming it came from the submission.

## Publication eligibility

A first publication attempt may proceed only when all of these are true:

1. the requested `RuleSubmission` exists,
2. its current status is exactly `approved`,
3. exactly one terminal review event exists for that submission,
4. that review event decision/to-status is exactly `approved`,
5. the review event's submission reference matches the requested submission,
6. the review event's draft digest matches the current recomputed stored submission snapshot digest,
7. if `RuleSubmission.DraftDigest` is non-NULL, it also matches the recomputed digest,
8. no publication event already exists unless the call resolves as the allowed exact replay,
9. current publication-time draft validation passes,
10. the database permits creation of the target unique `RiskRule.code`.

`pending` and `rejected` submissions are not publishable.

A missing/mismatched review event is an integrity failure, not permission to infer approval from `RuleSubmission.status` alone.

## Stored snapshot is the only publication source

The publication service must not accept rule draft fields from its caller.

Conceptually the command is limited to trusted publication attribution:

```go
type SubmissionPublicationCommand struct {
    ActorLabel string
}
```

The service loads all rule content from the stored approved `RuleSubmission`.

The caller must never supply or override:

- code,
- name,
- description,
- category code,
- rule type,
- pattern,
- weight,
- severity,
- enabled state,
- explanation,
- recommendation,
- source submission ID,
- draft digest,
- review event ID,
- publication event ID,
- published rule ID,
- actor kind,
- timestamps.

This prevents a valid approval for snapshot A from being reused to publish modified snapshot B.

## Publication-time revalidation

Approval-time validation is not sufficient forever.

Immediately before first publication, reconstruct the existing `DraftRequest` semantics from the stored approved submission and run current `ValidateDraft` inside the publication transaction context.

Publication must fail with **zero new RiskRule rows and zero publication events** when current validation fails.

Examples include:

- category removed after approval,
- another `RiskRule` with the code appeared after approval,
- supported rule types changed,
- supported severity values changed,
- regex validation changed/fails,
- weight/required-field policy changed,
- any other current `ValidateDraft` rule rejects the stored snapshot.

Do not re-run validation from a fresh client copy.

### Database uniqueness remains final authority

`ValidateDraft` performs a duplicate-code query, but that query alone cannot make publication concurrency-safe.

The existing database unique constraint on `RiskRule.code` is the final duplicate-code authority.

Therefore:

```text
validate duplicate code
-> attempt RiskRule insert
-> database unique constraint is final arbiter
```

If another publication wins the same code after validation but before insertion, the losing transaction must not create a publication event.

For two **different** approved submissions racing for the same code, the loser resolves as a publication validation/conflict outcome equivalent to current `duplicate_code`; it must not be treated as an exact replay of the winning submission.

## Draft digest and approval integrity

Publication must recompute the deterministic v1 digest from the stored `RuleSubmission` snapshot.

Freeze these checks:

### Normal rows

When `RuleSubmission.DraftDigest != NULL`:

```text
recomputed digest == RuleSubmission.DraftDigest
recomputed digest == approved ReviewEvent.DraftDigest
```

Both must hold.

### Legacy approved rows with NULL submission digest

The existing review design permits a legacy row whose `RuleSubmission.DraftDigest` is NULL while the terminal review event stores a computed digest.

Publication must:

- recompute from the stored snapshot,
- require equality with `RuleSubmissionReviewEvent.DraftDigest`,
- not backfill the legacy submission only for publication convenience.

### Integrity failure

Any disagreement among:

- stored snapshot,
- non-NULL submission digest,
- approved review event digest,
- review-event decision/status/source reference

is a publication integrity failure.

It must not be downgraded into an ordinary `duplicate_code` or generic validation conflict.

## RiskRule field mapping

The first published `RiskRule` must copy the approved stored snapshot exactly for current rule fields:

```text
Code           <- RuleSubmission.Code
Name           <- RuleSubmission.Name
Description    <- RuleSubmission.Description
CategoryCode   <- RuleSubmission.CategoryCode
RuleType       <- RuleSubmission.RuleType
Pattern        <- RuleSubmission.Pattern
Weight         <- RuleSubmission.Weight
Severity       <- RuleSubmission.Severity
Enabled        <- RuleSubmission.Enabled
Explanation    <- RuleSubmission.Explanation
Recommendation <- RuleSubmission.Recommendation
```

No publication-time hidden rewrite is allowed.

### `Enabled` behavior

Freeze publication behavior as:

```text
RiskRule.Enabled = RuleSubmission.Enabled
```

Do not force `true` and do not force `false`.

The pending snapshot already captured the draft's normalized/defaulted enabled value. Publication is the operation that creates the rule from that approved snapshot.

This means publishing a submission whose stored `Enabled=true` can make the new rule immediately active under existing engine behavior. Publication authorization is therefore a higher-impact privilege than review approval and must remain separately controlled.

If the project later wants a universal "publish disabled, activate separately" policy, that is a new design/migration decision and must not be introduced implicitly in implementation.

## Current-rule provenance: `source_submission_id`

Add one server-owned nullable provenance field to `RiskRule` conceptually equivalent to:

```go
SourceSubmissionID *uint `json:"-" gorm:"index"`
```

Exact GORM tags may be adjusted by implementation tests, but semantics are frozen:

- `NULL` means the current rule was not created by this controlled publication workflow (for example seed/manual/current direct-create paths),
- non-NULL means the current rule was originally created by publication of that `RuleSubmission`,
- the client must never bind/write this field,
- publication sets it to the approved source submission ID,
- current direct/manual `POST /rules` behavior must continue to create `RiskRule` rows with `source_submission_id = NULL`,
- existing seed rows remain compatible because the field is nullable.

The implementation should use a non-unique index for provenance lookup unless tests prove a stronger invariant is needed. The authoritative one-publication-per-submission invariant lives on the publication event.

### Why provenance also needs an event

`source_submission_id` helps answer:

> Where did this currently existing RiskRule originate?

It is not a full audit record and disappears if the RiskRule is hard-deleted.

The append-only publication event answers the historical question even after later rule deletion.

## Publication event model

The first implementation should add an audit entity conceptually equivalent to:

```go
type RuleSubmissionPublicationEvent struct {
    ID            uint      `gorm:"primaryKey"`
    SubmissionID  uint      `gorm:"not null;uniqueIndex"`
    ReviewEventID uint      `gorm:"not null;uniqueIndex"`
    RiskRuleID    uint      `gorm:"not null;index"`
    RiskRuleCode  string    `gorm:"size:120;not null;index"`
    ActorKind     string    `gorm:"size:40;not null"`
    ActorLabel    string    `gorm:"size:120;not null"`
    DraftDigest   string    `gorm:"size:64;not null"`
    CreatedAt     time.Time `gorm:"index"`
}
```

The implementation should add GORM associations/constraints for `SubmissionID` and `ReviewEventID` with restrictive delete behavior.

It must **not** add a first-slice foreign key from publication event to `RiskRule`.

### `SubmissionID`

Identifies the approved source submission.

Requirements:

- not null,
- unique in the first schema version,
- real FK to `RuleSubmission`,
- source submission deletion is restricted while publication history exists.

### `ReviewEventID`

Identifies the exact approval event that authorized publication.

Requirements:

- not null,
- unique in the first schema version,
- real FK to `RuleSubmissionReviewEvent`,
- review event deletion is restricted while publication history exists,
- service verifies that review event belongs to the same submission and represents approved terminal review.

The explicit review-event link avoids ambiguity if a future workflow later permits multiple review rounds.

### `RiskRuleID`

Stores the ID of the `RiskRule` created by the transaction.

It is historical audit data, not a first-slice FK.

Rationale:

- existing `RiskRule` lifecycle permits hard delete,
- publication audit must survive later deletion,
- silently adding `ON DELETE RESTRICT` would change existing rule lifecycle semantics as a side effect of publication.

The implementation may index this field for lookup but should not make correctness depend on the current rule row continuing to exist forever.

### `RiskRuleCode`

Stores the rule code at publication time.

This helps historical inspection if the corresponding rule is later deleted.

Do not impose lifetime uniqueness on publication-event rule code. Current `RiskRule.code` uniqueness remains an invariant of existing rows, not all historical publication records forever.

### `ActorKind`

Freeze first-version value as:

```text
controlled_publisher
```

This distinguishes publication attribution from review attribution (`controlled_maintainer`).

### `ActorLabel`

A trusted server-side operational publisher label.

Rules:

- trim whitespace,
- required,
- max 120 UTF-8 bytes,
- server-owned,
- never accepted from publication request JSON,
- not proof of a human user's authenticated personal identity.

### `DraftDigest`

Stores the recomputed digest of the exact approved submission snapshot used for publication.

It must match the approved review event digest at publication time.

It is a provenance fingerprint, not a digital signature, authorship proof, or tamper-proof ledger chain.

### `CreatedAt`

Publication timestamp generated by persistence.

Do not accept a client-provided timestamp.

## Append-only and honesty boundary

`RuleSubmissionPublicationEvent` is append-only in the first application design by:

- no update service,
- no delete service,
- no update/delete HTTP routes,
- no `UpdatedAt`,
- unique first publication per submission,
- restrictive source-submission/review-event relationships.

This is a strong normal-application audit contract.

It is **not** DBA-proof, cryptographically tamper-evident, non-repudiable, WORM storage, or an immutable ledger.

Do not market it as those things.

## Atomic publication transaction

The first internal publication service should conceptually expose:

```go
PublishApprovedSubmission(
    db *gorm.DB,
    submissionID uint,
    command SubmissionPublicationCommand,
) (SubmissionPublicationOutcome, error)
```

Conceptual first-publication flow:

1. Normalize/validate trusted publisher actor label.
2. Begin database transaction.
3. Load the `RuleSubmission` by ID.
4. If not found: return not found, zero writes.
5. Load existing publication event for the submission.
6. If publication already exists: resolve replay/conflict semantics; do not run a second publication.
7. Require current submission status `approved`.
8. Load the source `RuleSubmissionReviewEvent`.
9. Require it to be the matching approved terminal event for this submission.
10. Recompute stored snapshot digest.
11. Verify digest integrity against review event and non-NULL submission digest.
12. Reconstruct `DraftRequest` from stored submission.
13. Run current `ValidateDraft`.
14. If invalid: rollback/return publication-validation failure, zero writes.
15. Build `RiskRule` only from the stored snapshot and set server-owned `source_submission_id`.
16. Insert `RiskRule`.
17. Insert exactly one publication event with source submission, source review event, created rule ID/code, trusted actor, and digest.
18. If publication-event insertion fails: rollback RiskRule creation.
19. Commit.

The service must not mutate `RuleSubmission.status` during publication.

## Error classes for the internal service

The exact Go names may follow repository naming, but the next implementation must distinguish at least these semantic classes:

### Invalid publication command

Examples:

- nil DB,
- blank trusted actor label,
- actor label over 120 UTF-8 bytes.

No writes.

### Submission not found

Requested source submission does not exist.

No writes.

### Submission not publishable

Submission exists but current status is `pending`, `rejected`, or another unsupported state.

No writes.

### Publication validation failure

The approved stored snapshot no longer passes current `ValidateDraft`.

Return the structured `ValidationResult` internally so later transport design can choose a stable external shape without reparsing error text.

No writes.

### Publication conflict

Examples:

- publication already exists but trusted actor identity does not match exact replay semantics,
- another approved submission wins the same unique rule code,
- another non-identical publication attempt has already won.

No additional writes.

### Publication integrity failure

Examples:

- approved submission has no review event,
- review event belongs to another submission,
- review event is not approved,
- review-event digest disagrees with stored snapshot,
- non-NULL submission digest disagrees with stored snapshot,
- publication event exists but its source/review/digest metadata disagrees with the current historical source,
- publication event claims a rule/source relationship impossible under the service contract.

Integrity failures must not be converted to ordinary validation conflicts.

### Unexpected persistence failure

Any other database failure.

Atomic transaction guarantees must leave no half-publication.

## Exact publication replay

Publication replay exists only to make a retry of the same already-committed publication safe.

In the first controlled model, exact replay identity includes:

- submission ID,
- `actor_kind = controlled_publisher`,
- normalized trusted actor label.

The publication event itself supplies the original source review event, rule ID/code, digest, and timestamp.

### Same trusted publisher retry

When an event already exists and actor attribution matches:

- return the existing publication event,
- mark the outcome as replay,
- create no new `RiskRule`,
- create no new publication event,
- do not rerun publication as if it had never happened.

If the current `RiskRule` row still exists, the service may return it as current state in addition to the historical event.

### Different trusted publisher

A different actor label is not an exact replay.

Return publication conflict and do not add another event.

This prevents a later caller from being represented as the actor who performed the original publication.

## Published rule later changed or deleted

The repository already permits `RiskRule` update, enable-toggle, and hard delete. This design does not redesign those operations.

Therefore publication audit has a precise honesty boundary.

### Later update/toggle

If the published `RiskRule` is later modified or toggled:

- `source_submission_id` continues to identify its original publication source,
- the publication event continues to describe the initial published snapshot through its source/digest,
- publication does **not** claim that the current mutable rule still equals the approved source forever,
- replay must never overwrite later legitimate rule changes with the old submission snapshot.

A future rule-versioning or rule-change audit system is a separate task.

### Later hard delete

If the published `RiskRule` is later hard-deleted:

- publication event remains,
- source submission/review event remain protected by their restrictive relationships,
- `RiskRuleID`/`RiskRuleCode` remain historical identifiers in publication audit,
- retrying the old publication must **not** silently recreate the deleted rule,
- the service should return a deterministic already-published/current-rule-missing conflict or integrity-class result with zero writes.

Automatic republishing after deletion could unintentionally restore a deliberately removed or unsafe rule and is forbidden in the first slice.

## Concurrency contract

Correctness must hold across multiple backend processes.

Do not rely on an in-memory mutex.

### Same approved submission, same publisher

For concurrent identical calls:

- exactly one `RiskRule` is created,
- exactly one publication event is created,
- all other calls resolve to the same committed publication as replay,
- no caller creates a duplicate event/rule.

The implementation may encounter a database uniqueness error on a losing insert before the winner is visible. It must resolve the committed publication outside/after the failed transaction rather than returning a misleading second publication.

### Same approved submission, different publishers

Exactly one actor wins the first publication.

Other actor labels return conflict.

There is no voting, maker-checker, quorum, or multi-publisher attribution in the first version.

### Different approved submissions, same rule code

Exactly one `RiskRule.code` may win because the existing database unique constraint is authoritative.

For the losing submission:

- no publication event,
- no `RiskRule` from that submission,
- source remains approved/unpublished,
- caller receives publication validation/conflict semantics consistent with code already existing.

Do not reinterpret the other submission's publication as this submission's replay.

## Interaction with pending submission replay

The existing pending-digest model remains unchanged.

Publication does not create lifetime digest uniqueness.

While the published `RiskRule` exists, current `ValidateDraft` naturally rejects a new submission using the same rule code.

If a published rule is later deleted, current validation may again permit a same-code/new submission according to the rules that exist at that future time. This publication design does not add a permanent tombstone for rule codes.

## Future publication authorization boundary

The internal publication persistence implementation should remain service-only first.

Before any HTTP publication route exists, the project must add an independent default-off publication gate/credential conceptually equivalent to:

```text
RULE_SUBMISSION_PUBLICATIONS_ENABLED=false
RULE_SUBMISSION_PUBLICATION_TOKEN=<independent secret>
RULE_SUBMISSION_PUBLICATION_ACTOR_LABEL=<non-secret trusted label>
```

Freeze these security requirements:

- publication token is independent from `RULE_SUBMISSION_WRITE_TOKEN`,
- publication token is independent from `RULE_SUBMISSION_REVIEW_TOKEN`,
- when publication is enabled, configuration fails closed if token/actor label is invalid,
- token minimum should follow the current controlled-token baseline (at least 32 characters),
- actor label is trimmed, required, max 120 UTF-8 bytes,
- actor label is server-owned,
- publication token never appears in logs/responses,
- write/review credentials must not authorize publication,
- publication actor kind is `controlled_publisher`, not `controlled_maintainer`,
- this credential model is not OAuth/RBAC or proof of a specific person's identity.

The exact HTTP route/body/status mapping is **not frozen by this domain design**. A separate bounded publication transport design must precede exposing the mutation endpoint.

## No publication Redis limiter in the persistence slice

Do not copy the submission-write Redis rate limiter into internal publication persistence.

Publication-specific abuse control is a transport/security decision and should be designed only when the publication route is exposed.

The absence of a Redis limiter in service-only persistence must not be interpreted as permission for an unauthenticated HTTP route.

## Logging and data safety

Allowed publication operational metadata may include:

- submission ID,
- review event ID,
- resulting risk rule ID/code,
- created/replay/conflict outcome,
- validation field/error codes,
- trusted actor label only if operators classify it as non-secret.

Do not log by default:

- publication token,
- review/write tokens,
- raw rule pattern or free text,
- full submission snapshot,
- canonical draft JSON,
- raw review reason,
- draft digest unless needed for tightly controlled integrity diagnostics.

Publication is not an evidence-upload channel.

## Database model and AutoMigrate boundary

The current repository uses `AutoMigrate`.

The next implementation should therefore keep schema evolution additive and compatible with existing rows:

- add nullable `RiskRule.source_submission_id`,
- add `RuleSubmissionPublicationEvent`,
- preserve existing `RiskRule` rows with NULL source provenance,
- preserve existing review/submission tables and statuses,
- do not destructively rename/drop existing columns,
- do not add a global `RuleSubmission.status` check as an unrelated side effect,
- do not replace current pending digest/index design.

PostgreSQL 16 integration tests and SQLite compatibility tests remain mandatory.

## Required implementation test matrix

The next internal publication persistence PR is not complete unless automated tests cover at least:

1. approved submission + approved review event publishes one `RiskRule`,
2. publication copies all stored snapshot rule fields exactly,
3. stored `Enabled=true` publishes true and stored `Enabled=false` publishes false,
4. publication-created rule has server-owned `source_submission_id`,
5. direct/manual existing rule creation keeps `source_submission_id = NULL`,
6. one publication event records submission ID, review event ID, risk rule ID/code, actor kind/label, digest, timestamp,
7. pending submission cannot publish and writes zero rule/event rows,
8. rejected submission cannot publish and writes zero rule/event rows,
9. nonexistent submission writes zero rows,
10. approved submission missing review event fails integrity and writes zero rows,
11. non-approved/mismatched review event fails integrity and writes zero rows,
12. review-event digest mismatch fails integrity and writes zero rows,
13. non-NULL submission digest mismatch fails integrity and writes zero rows,
14. legacy approved NULL submission digest may publish only when recomputed digest matches approved review event,
15. category removed after approval -> current validation failure, zero writes,
16. existing RiskRule with same code -> current validation failure, zero publication writes,
17. other current validation failure -> zero writes,
18. publication-event insert failure rolls back RiskRule creation,
19. exact same-actor retry returns same event/rule identity and creates no duplicate,
20. different actor after publication returns conflict and creates no duplicate,
21. concurrent same-submission/same-actor publication has one rule/event winner and safe replays,
22. concurrent same-submission/different-actor publication has one winner and conflicts for other actor,
23. two approved submissions racing for the same code produce exactly one RiskRule and one winning publication event,
24. losing same-code submission remains approved with no publication event,
25. publication event's source submission FK rejects normal deletion of the source submission,
26. publication event's review-event FK rejects normal deletion of the source approval event,
27. later RiskRule update/toggle does not rewrite publication event/source digest,
28. later RiskRule hard delete does not delete publication event,
29. retry after published RiskRule was hard-deleted does not recreate the rule,
30. no publication path mutates review decision/reason/event,
31. no failed publication mutates `RuleSubmission.status`,
32. race detector keeps publication/review critical packages green,
33. existing review PostgreSQL concurrency/rollback/FK tests remain green,
34. repository-wide GitHub Actions backend Test/Race/Vet and frontend Install/Typecheck/Build remain green.

## PostgreSQL concurrency acceptance

At least one real PostgreSQL integration test must prove same-submission publication concurrency across independent DB sessions/connections.

Recommended test shape:

```text
12 concurrent PublishApprovedSubmission calls
same approved submission
same trusted actor
=> exactly 1 RiskRule
=> exactly 1 publication event
=> all successful outcomes reference the same publication
=> no data race
```

A separate PostgreSQL test should race two independently approved submissions carrying the same rule code and prove the `RiskRule.code` unique constraint produces exactly one winner.

SQLite tests alone are insufficient evidence for cross-process publication correctness.

## Future read/provenance contract

The first persistence implementation should provide internal read helpers sufficient to inspect:

- publication event by submission ID,
- publication event by review event ID,
- publication history identifiers for a RiskRule ID/code,
- current RiskRule provenance source when `source_submission_id` is non-NULL.

These helpers remain internal/read-only in the persistence slice.

A maintainer-facing read HTTP API is a separate bounded task.

## Out of scope / explicitly deferred

This first publication contract does **not** design or implement:

- publication HTTP transport details,
- publication Redis limiter,
- OAuth,
- RBAC,
- named authenticated publisher users,
- maker-checker / two-person publication,
- multi-review consensus,
- automatic publication immediately after approval,
- automatic republish after rule deletion,
- rule versioning,
- audit of later RiskRule edit/toggle/delete operations,
- soft-delete conversion,
- rollback/unpublish workflow,
- superseding one publication with another,
- cryptographic audit chaining,
- public community publication access,
- frontend publication UI.

Each requires its own bounded design if/when needed.

## Implementation boundary after this design

After this document is merged, the next bounded code task should be **internal publication persistence only**.

Expected production scope is approximately:

- nullable server-owned `RiskRule.SourceSubmissionID`,
- `RuleSubmissionPublicationEvent` model + restrictive source FKs,
- AutoMigrate wiring,
- `PublishApprovedSubmission` service,
- internal publication read helpers,
- focused SQLite tests,
- focused PostgreSQL concurrency/integrity tests.

It should **not** add:

- publication config fields,
- publication authorization middleware,
- publication HTTP route/handler,
- Redis limiter,
- frontend changes.

Those belong to later bounded increments.

## Acceptance decision

After this design is merged, the publication persistence/audit contract is sufficiently frozen for an internal implementation without inventing new core business, provenance, replay, concurrency, or security semantics during coding.
