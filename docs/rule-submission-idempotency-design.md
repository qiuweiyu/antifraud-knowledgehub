# Rule Submission Exact Replay and Idempotency Design

This document freezes the next bounded safety step for controlled rule submissions: exact-replay suppression that is deterministic, race-safe, and compatible with the existing `RuleSubmission` persistence model.

It is a **design-only** step for Issue #31. No database column, index, handler behavior, migration code, reviewer state, contributor identity, approval path, publication path, frontend UI, bot, or AI review is implemented by this document.

## Decision summary

1. Exact replay is defined from a **server-canonicalized persisted draft snapshot**, not from raw request bytes.
2. The canonical representation is versioned as `afkh-rule-submission-draft:v1`.
3. The replay key is `SHA-256(version-prefix + canonical-json)` encoded as lowercase hexadecimal.
4. The digest includes every persisted draft field and excludes system-owned fields such as `id`, `status`, and timestamps.
5. `code` remains non-unique; same code with different draft content remains a distinct proposal.
6. Replay suppression applies to the **pending queue**, not to all historical submissions forever.
7. The database unique invariant, not a read-before-write check, is the concurrency authority.
8. First creation returns `201 Created`; an exact replay returns the existing pending submission with `200 OK`.
9. Replay detection occurs after feature gating, authorization, and rate limiting. Replays still consume rate-limit budget.
10. Existing rows must be migrated without deleting or rewriting historical draft content.
11. The implementation must add real database concurrency tests before merge.

## Why raw request bytes are not the replay identity

Two JSON requests can represent the same persisted draft while differing only in transport syntax:

- JSON field order,
- insignificant whitespace,
- `enabled` omitted versus explicitly set to `true`,
- leading/trailing whitespace in fields that the existing draft mapper already trims.

Therefore hashing the raw HTTP body would create false negatives and would couple idempotency to client serialization details.

Replay identity must instead be derived from the exact server-side snapshot that would be inserted into `RuleSubmission`.

## Canonical draft representation

The implementation should derive canonical values from the same mapping already used by `CreatePendingSubmission` / `DraftRequest.riskRule()`.

Conceptually, use a dedicated struct with a fixed field order:

```go
type canonicalSubmissionDraftV1 struct {
    Code           string `json:"code"`
    Name           string `json:"name"`
    Description    string `json:"description"`
    CategoryCode   string `json:"category_code"`
    RuleType       string `json:"rule_type"`
    Pattern        string `json:"pattern"`
    Weight         int    `json:"weight"`
    Severity       string `json:"severity"`
    Enabled        bool   `json:"enabled"`
    Explanation    string `json:"explanation"`
    Recommendation string `json:"recommendation"`
}
```

Do not build canonical JSON from a map. A fixed struct field order makes the byte representation deterministic inside this Go service.

### Normalization rules inherited from the current mapper

The following fields already use trimmed persisted values and therefore must be hashed after that same normalization:

- `code`
- `name`
- `category_code`
- `rule_type`
- `pattern`
- `severity`

`enabled` must be canonicalized to the persisted boolean value. Therefore:

- omitted `enabled` -> `true`
- explicit `enabled: true` -> `true`

These two inputs are exact replays.

The current persistence mapper does **not** trim these free-text fields, so v1 must preserve them byte-for-byte as stored:

- `description`
- `explanation`
- `recommendation`

A whitespace change in one of those fields is therefore a different proposal in v1. Do not silently add new normalization only for the digest, because that would make replay identity diverge from the persisted snapshot.

## Versioned digest algorithm

The digest must be internal and versioned so future schema changes can define a v2 without reinterpreting old hashes.

Algorithm:

1. Build the canonical v1 struct from the server-normalized persisted snapshot.
2. Encode it with Go `encoding/json`.
3. Prefix the bytes with:

```text
afkh-rule-submission-draft:v1\n
```

4. Compute SHA-256 over `prefix || canonicalJSON`.
5. Store lowercase hexadecimal, exactly 64 ASCII characters.

Conceptually:

```go
payload := append([]byte("afkh-rule-submission-draft:v1\n"), canonicalJSON...)
sum := sha256.Sum256(payload)
digest := hex.EncodeToString(sum[:])
```

The digest is **not**:

- authentication,
- contributor identity,
- a secret,
- proof of authorship,
- semantic similarity detection.

It is only an exact persisted-draft fingerprint.

## Fields intentionally excluded

Do not include system-owned or environment-derived fields:

- submission `id`,
- `status`,
- `created_at`,
- `updated_at`,
- Authorization token,
- Redis limiter state,
- IP address or forwarding headers,
- validation warnings,
- current category metadata beyond the submitted `category_code`,
- current `RiskRule` state.

Including any of these would make a retry produce a different digest even though the contributor submitted the same draft.

## Duplicate semantics

### Exact replay

Two requests are exact replays when their canonical v1 persisted snapshots produce the same digest.

Examples that **must** replay to the same pending submission:

- identical drafts,
- different JSON property ordering,
- trimmed-field transport whitespace that produces the same persisted value,
- omitted `enabled` and explicit `enabled: true`.

### Distinct proposal

A changed persisted draft must receive a different digest.

Examples:

- same `code`, different `pattern`,
- same `code`, different `weight`,
- same `code`, different `severity`,
- `enabled: true` versus `enabled: false`,
- changed description/explanation/recommendation,
- a free-text whitespace change that is persisted by the current mapper.

Therefore **same code does not mean duplicate**.

This preserves the existing design decision that competing or revised proposals may share a rule code while pending.

## Pending-scoped uniqueness

Replay suppression should prevent duplicate entries in the active review queue without permanently declaring a digest globally unique for all future workflow states.

The target database invariant is conceptually:

```sql
CREATE UNIQUE INDEX ux_rule_submissions_pending_digest
ON rule_submissions (draft_digest)
WHERE status = 'pending' AND draft_digest IS NOT NULL;
```

Both PostgreSQL and the project's SQLite development/test path support partial indexes.

Why pending-scoped instead of global uniqueness:

1. Future review states are not frozen yet.
2. An identical proposal may need to be resubmitted after a future terminal state under rules that do not exist today.
3. Future contributor identity may require replay scope to be revisited.
4. We should solve current pending-queue duplication without prematurely freezing lifetime community identity semantics.

The index is therefore a **current queue invariant**, not a permanent global authorship rule.

## Model shape for the next implementation

Add a server-owned digest field conceptually equivalent to:

```go
DraftDigest *string `json:"-" gorm:"size:64"`
```

Important points:

- do not accept `draft_digest` from client JSON,
- do not expose it as part of the public submission response in the first implementation,
- keep it nullable during migration so legacy rows can be handled safely,
- all newly created pending submissions must receive a non-null 64-character digest through service code.

Do **not** place a partial unique GORM tag directly on the field if that would cause `AutoMigrate` to create the unique index before legacy backfill is complete.

## Safe migration of existing rows

The project already has `RuleSubmission` rows created before digest support may exist in user databases. The next implementation must not assume an empty table.

A safe startup/schema sequence is:

1. Add the nullable `draft_digest` column through the existing schema path.
2. Read legacy pending submissions in deterministic `created_at, id` order.
3. Compute the canonical v1 digest from each **stored snapshot**, without changing draft content.
4. For each digest group:
   - assign the digest to the earliest row,
   - leave later pre-existing exact duplicates with `draft_digest = NULL`,
   - do not delete, merge, or rewrite those legacy rows.
5. Create the partial unique index only after backfill succeeds.
6. If index creation or backfill fails unexpectedly, startup/schema preparation must fail visibly rather than silently disabling concurrency protection.

This approach preserves historical rows while establishing an authoritative guard for all new writes.

Legacy null duplicates remain visible to maintainers but cannot cause new duplicate growth. A later maintenance task may explicitly annotate or consolidate them only after review/audit semantics exist.

## Why application pre-check alone is insufficient

This is unsafe:

```text
SELECT pending by digest -> none
SELECT pending by digest -> none
INSERT A
INSERT B
```

Two concurrent requests can both observe absence and create duplicate rows.

Therefore an early lookup is only an optimization/replay fast path. The database partial unique index is the final concurrency authority.

## Create/replay service flow

The future service should behave conceptually as follows:

1. Build the canonical snapshot and digest.
2. Look for an existing pending row with that digest.
3. If found, return it as a replay without writing.
4. If no row exists, run current `ValidateDraft`.
5. If validation fails, return validation errors and perform zero writes.
6. Attempt to insert one `pending` submission containing the digest.
7. If insert succeeds, return `created=true`.
8. If insert fails, immediately query pending by the digest:
   - if found, another concurrent request won the race; return that row as replay,
   - if not found, return the original database error.

This post-error lookup avoids relying solely on database-driver-specific unique-error parsing while still letting the database enforce concurrency.

Do not use a mutex or in-memory map as the authority. Multiple server processes must receive the same result.

## Replay before revalidation

If a pending submission with the same digest already exists, return that existing submission **before running current validation again**.

Reason: a network retry should remain idempotent even if mutable validation dependencies changed after the first accepted request, for example:

- a category was later renamed/removed,
- a new `RiskRule` with the same code was introduced,
- validation rules became stricter.

Returning an existing pending snapshot creates no new trust or write. Future approval must still revalidate against current rules, as already required by the persistence design.

A new digest, however, must always pass current validation before insertion.

## HTTP response semantics

Keep the existing response envelope and submission object shape.

- first successful creation -> `201 Created`
- exact replay of an existing pending submission -> `200 OK`
- validation failure for a new digest -> existing `400` behavior
- database failure with no concurrent winner -> server error using the existing error envelope

The replay response must return the **same existing submission ID**.

Do not return `409 Conflict` for an exact replay. A successful retry is not a conflict.

No new client-supplied `Idempotency-Key` header is required in this slice. The canonical digest solves exact-body replay independently of client cooperation.

## Ordering with existing transport controls

The current transport order remains:

```text
feature flag
-> Bearer authorization
-> Redis rate limiter
-> bounded strict JSON parsing
-> exact replay / validation / persistence
```

Replay detection must **not** move ahead of authorization or rate limiting.

Exact retries still consume rate-limit capacity. Otherwise a valid captured request could be replayed without normal abuse-control cost.

## Logging

The digest is not secret, but the first implementation should not log it by default because it creates an unnecessary stable fingerprint of submitted content.

Allowed metadata remains:

- request route/status/latency,
- replay versus created outcome,
- resulting submission ID,
- validation field/error codes.

Do not log:

- raw body,
- Authorization token,
- canonical JSON,
- `pattern`, description, explanation, or recommendation,
- digest unless a future explicit diagnostic requirement is designed.

## Future contributor identity caveat

The current controlled transport uses a maintainer-held write credential and has no contributor identity model.

The pending-scoped digest uniqueness therefore intentionally deduplicates identical active proposals regardless of who sent the controlled request.

Before true public contributor identity/OAuth is added, revisit whether replay scope should remain queue-global or become contributor-scoped. Do not reinterpret this v1 digest as contributor identity.

## Next implementation acceptance criteria

The next code PR should be complete only if all of the following are true:

1. Canonicalization is implemented in one dedicated helper with a version constant.
2. Digest is exactly 64 lowercase hex characters from SHA-256.
3. Omitted `enabled` and explicit `true` produce the same digest.
4. Existing trimmed persisted fields produce stable digest equivalence.
5. Any changed persisted field produces a different digest.
6. New pending rows always receive a server-computed digest.
7. Existing rows can be upgraded without deletion or content mutation.
8. Legacy exact duplicates do not prevent the new unique pending guard from being installed.
9. Sequential exact replay returns the same ID and leaves submission count unchanged.
10. Same code with different content creates a distinct pending proposal.
11. Exact replay can return the existing row even if current validation would now fail.
12. Concurrent identical creates result in exactly one new digested pending row and all callers resolve to the same submission ID.
13. Concurrency correctness comes from the database unique invariant, not an in-process lock.
14. No `RiskRule` is created or modified by create or replay paths.
15. Rate limiting remains ahead of replay detection.
16. Public response does not accept or trust a client-provided digest.
17. `go test ./...` and `go vet ./...` pass.
18. Existing frontend install/typecheck/build gates remain green.
19. Redis integration tests remain green.
20. Add real PostgreSQL CI coverage for the partial unique index / concurrent create path, because this invariant is database-concurrency-sensitive and PostgreSQL is the default production database.

## Required test matrix for implementation

### Canonicalization unit tests

- same canonical snapshot -> same digest,
- JSON input field order cannot affect digest,
- surrounding whitespace on trimmed fields -> same digest,
- omitted enabled versus true -> same digest,
- false enabled -> different digest,
- each persisted field change -> different digest,
- deterministic known digest fixture for v1 to detect accidental algorithm drift.

### Persistence tests

- first valid create -> one pending row,
- sequential exact replay -> same ID, still one row,
- same code / different draft -> two rows,
- invalid new digest -> zero new rows,
- replay after mutable validator dependency changes -> existing ID, zero new rows,
- `RiskRule` count unchanged throughout.

### Legacy upgrade tests

Build a pre-digest table/data fixture, then run the new schema preparation:

- no rows deleted,
- no draft fields mutated,
- earliest exact duplicate becomes the guarded representative,
- later legacy exact duplicates remain nullable/unmodified except schema addition,
- partial unique index is installed,
- subsequent exact retry resolves to the representative.

### Concurrency tests

Run many concurrent identical creates against a real database:

- exactly one newly guarded pending row,
- all successful callers receive the same submission ID,
- no duplicate non-null pending digest,
- no `RiskRule` side effects,
- no process-local locking assumption.

SQLite can retain lightweight behavior coverage, but the concurrency/partial-index invariant must also run against a PostgreSQL service in GitHub Actions before merge.

## Explicit non-goals

The next implementation must not add:

- OAuth or public contributor identity,
- reviewer roles,
- approve/reject/change-requested transitions,
- publication to `RiskRule`,
- rule version history,
- contributor reputation,
- attachments or evidence blobs,
- browser extension integration,
- GitHub bots,
- AI review,
- semantic/fuzzy duplicate detection.

Semantic similarity is a separate moderation problem. This slice only makes exact retries safe.

## Follow-up sequence

After this design:

1. **Exact replay/idempotency implementation** — canonical digest + safe legacy schema preparation + race-safe pending uniqueness + tests.
2. **Maintainer review state design** — freeze approve/reject semantics and audit history.
3. **Review implementation** — explicit human transitions, still no automatic publication.
4. **Publication design/implementation** — revalidate then create `RiskRule` only after explicit approval.
5. **Community identity design** — replace the controlled maintainer write token only after identity and abuse boundaries are ready.

This keeps Issue #1 incremental while making the newly exposed controlled submission endpoint safe under retries and concurrency.