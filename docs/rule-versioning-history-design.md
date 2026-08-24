# Rule Versioning and History Design

Status: **Design First / frozen contract for implementation**

This document defines the first trustworthy Rule versioning/history model for the broader `v0.2 Community Rules` roadmap.

It is intentionally a design-only artifact. It does not itself change runtime code, schema, API routes, frontend behavior, seed data, credentials, or the deterministic scoring algorithm.

The design extends the existing controlled workflow:

```text
proposal -> pending submission -> human review -> controlled publication
```

without reintroducing anonymous mutation of executable `RiskRule` records.

The current public Category / Rule / Case persisted-content API remains read-only. AI remains supplemental and has no authority to approve, publish, revise, enable, disable, retire, or delete a Rule.

## 1. Existing contracts that remain authoritative

This design builds on current `main` rather than inventing a parallel rule-management path.

Existing facts:

- `RiskRule` is the current executable rule projection used by the deterministic risk engine.
- `RiskRule.code` is unique.
- public direct Rule create/update/toggle/delete routes are absent.
- `POST /api/v1/rules/validate` is a public no-write create-draft validator.
- `RuleSubmission` stores the reviewed draft snapshot separately from executable Rules.
- review is terminal `pending -> approved|rejected` and writes one `RuleSubmissionReviewEvent`.
- approval does not mutate a `RiskRule`.
- publication is separately authorized/default-off and currently creates the initial `RiskRule` plus one `RuleSubmissionPublicationEvent`.
- one submission and one review event may each have at most one publication event.
- `RuleSubmissionPublicationEvent.risk_rule_id` is indexed but is deliberately not unique.
- `RiskRule.source_submission_id` records the submission that originally created a Rule through controlled publication; it is not a current-version pointer.
- publication/review correctness is transaction/database-constraint based, not process-mutex based.
- the repository currently uses GORM `AutoMigrate` plus explicit preparation helpers instead of numbered migrations.
- seed import uses `FirstOrCreate`; it does not overwrite existing Rule rows.
- historical `AnalysisRecord.matched_rules` JSON currently has no Rule version field.

Where older publication documents say that later Rule updates/toggles are outside publication audit, this design deliberately supersedes that **for versioning-era controlled revisions**. Historical first-publication events are not rewritten.

## 2. Problem statement

A mutable `RiskRule` row with only `updated_at` cannot answer:

- what exact rule snapshot existed before a change,
- which approved submission produced a change,
- which version an analysis used,
- whether two maintainers published conflicting changes from the same base,
- whether a later retry refers to an already-published historical revision,
- whether a legacy Rule state is reconstructed evidence or merely the first state observed after versioning was introduced.

Version history must not fabricate answers to those questions.

## 3. Decision summary

The implementation must follow these decisions.

1. `RiskRule.ID` is the stable database identity of one logical Rule.
2. `RiskRule` remains the current executable projection; the engine does not scan historical versions.
3. Add a positive current version number to `RiskRule`, starting at `1` for the first known version.
4. Add an immutable-by-application-contract `RiskRuleVersion` table containing full snapshots.
5. Versions are monotonically increasing positive integers per `RiskRule.ID` with a database unique constraint on `(risk_rule_id, version)`.
6. Version numbers are contiguous for history known to this system; legacy gaps are represented by source metadata, not invented missing rows.
7. From the versioning freeze forward, Rule `code` is immutable for controlled revisions.
8. If a code must change after the freeze, create a new logical Rule and retire/disable the old Rule; do not rename it through a revision.
9. Historical pre-freeze evidence may reveal a code change that occurred before this invariant existed. Such evidence must not be rewritten.
10. `RiskRule.source_submission_id` keeps its original meaning and is never changed to point at the latest revision.
11. Each controlled publication, including a revision, still has exactly one `RuleSubmissionPublicationEvent` for its source submission/review.
12. A `RiskRuleVersion` may link to the publication event that established it; one publication event may establish at most one Rule version.
13. New-rule publication creates Rule version `1` in the same transaction as the current Rule and publication event.
14. Rule revisions use the same independent submission/review/publication trust boundaries; no new weaker mutation path is added.
15. A revision proposal explicitly targets one `RiskRule.ID` and one `base_version`.
16. Revision draft clients cannot choose a new Rule code; the server copies the target's current code into the stored submission snapshot.
17. A no-op revision that produces the same canonical snapshot as its base is rejected.
18. Revision approval revalidates the target and requires the recorded base version to still be current.
19. Revision publication revalidates again and uses a conditional version compare-and-swap; stale approved revisions fail rather than overwriting a newer Rule.
20. Two different revision submissions from the same base may exist and may even be independently approved, but at most one can publish that next version.
21. Same-submission publication replay resolves the exact historical version established by that submission, even if newer Rule versions now exist.
22. A different publisher actor retry remains a conflict under the existing publisher-attribution contract.
23. `enabled` is ordinary versioned Rule content. Disabling or re-enabling through the controlled lifecycle creates a new version.
24. No controlled hard-delete operation is introduced. Retirement in this slice means publishing a revision with `enabled=false`.
25. Existing out-of-band/legacy hard deletion is not retroactively denied by a new foreign key from history to `RiskRule`; history must survive current-row deletion.
26. Public history reads are allowed because they are side-effect free, but first-slice public history responses do not expose trusted actor labels, credentials, or internal review rationale.
27. New deterministic matched-rule results include the exact positive Rule version used for the match.
28. Existing `AnalysisRecord` rows are not rewritten to invent a Rule version they never stored.
29. Legacy/backfill history explicitly distinguishes proven controlled-publication snapshots from `legacy_baseline` snapshots observed during migration.
30. Database constraints, transactions, and conditional updates are correctness authorities. In-memory locks are not.

## 4. Stable identity and code policy

### 4.1 Stable identity

The stable logical identity is:

```text
RiskRule.ID
```

History APIs and version uniqueness are keyed by this ID.

Do not use `code` as the database history identity. A pre-versioning installation may have allowed an old direct update to alter a code before the new immutability rule existed; version history must be able to represent that fact honestly.

### 4.2 Code immutability after the freeze

For every versioning-era controlled revision:

```text
revision.Code == current RiskRule.Code
```

The revision HTTP request does not contain a client-controlled `code` field. The server copies the target Rule's code into the stored `RuleSubmission` snapshot.

If maintainers intentionally need a different code:

```text
create new Rule with new code
+
optionally publish enabled=false revision of old Rule
```

This avoids silently breaking external references, publication audit, analysis records, or per-code lookup behavior.

## 5. Current projection model

`RiskRule` remains the row queried by the risk engine.

Add conceptually:

```go
CurrentVersion uint `json:"version" gorm:"not null;default:1"`
```

The exact Go field name may be `Version` or `CurrentVersion`, but the public JSON field for the current Rule is `version`.

Rules visible to the engine therefore remain one row per logical Rule:

```text
RiskRule ID=42
code=safe_account_transfer
version=3
...current v3 fields...
```

The engine must not query all historical version rows during normal analysis.

## 6. Immutable version model

Add a history entity conceptually equivalent to:

```go
type RiskRuleVersion struct {
    ID                 uint
    RiskRuleID         uint
    Version            uint

    Code               string
    Name               string
    Description        string
    CategoryCode       string
    RuleType           string
    Pattern            string
    Weight             int
    Severity           string
    Enabled            bool
    Explanation        string
    Recommendation     string

    SourceKind         string
    SourceSubmissionID *uint
    ReviewEventID      *uint
    PublicationEventID *uint
    SnapshotDigest     string
    CreatedAt          time.Time
}
```

### 6.1 Required database invariants

At minimum:

```text
risk_rule_id NOT NULL
version > 0
UNIQUE(risk_rule_id, version)
code NOT NULL
snapshot_digest NOT NULL
source_kind IN ('controlled_publication', 'legacy_baseline')
```

`publication_event_id`, when non-NULL, must be unique.

For `source_kind='controlled_publication'`, application preparation/publication logic requires all of:

```text
source_submission_id != NULL
review_event_id != NULL
publication_event_id != NULL
```

For `source_kind='legacy_baseline'`, those three source IDs are NULL.

The implementation may add cross-field CHECK constraints if they are demonstrated compatible with both PostgreSQL and the repository's SQLite test/development path.

### 6.2 No foreign key to current `RiskRule`

`RiskRuleVersion.risk_rule_id` is historical identity data and must not use a restrictive foreign key to `RiskRule` in this first slice.

Reason:

- earlier repository contracts permitted hard deletion,
- publication audit intentionally survives current Rule deletion,
- adding an `ON DELETE RESTRICT` relationship as a side effect would silently redefine an older lifecycle contract,
- history should remain inspectable after an out-of-band/legacy current-row deletion.

This does **not** create a supported hard-delete API.

### 6.3 Snapshot digest

Add a domain-separated Rule-snapshot digest, for example:

```text
afkh-risk-rule-snapshot:v1
```

It covers the canonical versioned content fields:

```text
code
name
description
category_code
rule_type
pattern
weight
severity
enabled
explanation
recommendation
```

It does not include database IDs, version number, source IDs, actor labels, or timestamps.

Do not silently reuse the existing `afkh-rule-submission-draft:v1` domain string for a different persistence purpose.

The content equality of a controlled version and its approved submission may be verified field-by-field and/or by computing the corresponding canonical snapshot under each domain.

## 7. Legacy migration/backfill: no fabricated history

Versioning is introduced after mutable Rules have already existed. The migration must distinguish what can be proven from what can only be observed now.

Add an idempotent preparation step after `AutoMigrate`, conceptually:

```text
PrepareRiskRuleVersionHistory(db)
```

### 7.1 Seed/manual/current Rules without controlled publication provenance

For a current Rule with:

```text
source_submission_id IS NULL
```

and no existing version history, create:

```text
version = 1
source_kind = legacy_baseline
snapshot = current RiskRule fields at preparation time
```

This means:

> This is the first state versioning can prove/observe.

It does **not** mean:

> This was necessarily the original state of this Rule when first created.

### 7.2 Controlled-publication Rule whose current snapshot still equals its approved source

If a current Rule has consistent:

- `source_submission_id`,
- approved review event,
- publication event,
- publication event Rule ID/code,
- approved submission digest,
- current Rule snapshot equal to the originally published submission snapshot,

backfill one version:

```text
v1 = controlled_publication snapshot
```

linked to the historical source/review/publication event.

### 7.3 Controlled-publication Rule that changed before versioning

If the original controlled publication can be proven but the current Rule snapshot no longer equals that original approved snapshot, do **not** rewrite either fact.

Backfill:

```text
v1 = original approved controlled-publication snapshot
v2 = current observed snapshot, source_kind=legacy_baseline
current RiskRule.version = 2
```

This explicitly records that an unversioned lifecycle change existed between the known publication and the versioning freeze, even if the exact time/actor/path of that historical mutation cannot be reconstructed.

A legacy v2 snapshot may contain a different code if such a pre-freeze mutation actually happened. From the freeze forward the then-current code becomes immutable for controlled revisions.

### 7.4 Publication event whose current Rule is already missing

If a valid historical publication event/source/review exists but its current `RiskRule` row was hard-deleted before versioning, preparation may reconstruct only the provable historical controlled-publication version from the approved submission snapshot.

It must not recreate the executable `RiskRule` row.

The history row remains keyed by the publication event's historical `risk_rule_id`.

### 7.5 Integrity failures during preparation

Fail closed rather than guessing when examples such as these occur:

- `source_submission_id` points to a missing/inconsistent source,
- publication and review events disagree about the source,
- a controlled publication digest does not match the approved stored snapshot,
- more than one pre-versioning publication event claims an impossible first-publication shape for the same source,
- an existing history row conflicts with the recomputed canonical snapshot,
- the current Rule version claims a latest version whose snapshot disagrees with the current projection.

Preparation is idempotent. Re-running it on a consistent database must add no duplicate versions and must not renumber existing history.

## 8. Submission intent model

The existing create-submission contract remains supported.

Extend stored `RuleSubmission` with server-owned/versioning context conceptually:

```text
kind                create | revision
target_risk_rule_id nullable historical Rule ID
base_version        nullable positive version
request_digest      nullable/then backfilled idempotency digest
```

Shape invariant:

```text
kind=create   => target_risk_rule_id NULL AND base_version NULL
kind=revision => target_risk_rule_id NOT NULL AND base_version > 0
```

Existing rows are backfilled as:

```text
kind=create
```

because the pre-versioning transport only supported new-rule proposals.

Do not add a foreign key from revision target to current `RiskRule`; an approved/rejected historical submission must remain auditable if a current Rule later disappears.

## 9. Pending-submission idempotency

The existing `draft_digest` remains the digest of the exact stored rule-content snapshot and remains authoritative for review/publication integrity.

It is no longer sufficient as the sole pending-request identity because the same content can have different mutation intent/target/base semantics.

Add a separate domain-separated request digest, conceptually:

```text
afkh-rule-submission-request:v1
```

covering:

```text
kind
target_risk_rule_id
base_version
draft_digest
```

Migrate pending uniqueness from:

```text
UNIQUE draft_digest WHERE status='pending'
```

to:

```text
UNIQUE request_digest WHERE status='pending'
```

with explicit preparation/backfill.

Consequences:

- identical create draft retries still replay one pending create submission,
- identical revision target/base/draft retries replay one pending revision submission,
- the same content proposed for different Rules or different base versions does not collide,
- different revision drafts for the same target/base may coexist and compete at review/publication time.

Existing historical `draft_digest` values and review/publication event digests are not rewritten.

## 10. Revision proposal transport

Do not overload the existing flat create-submission HTTP body with optional client-controlled target metadata.

Add a separate default-off protected route under the existing submission-write trust boundary, conceptually:

```text
POST /api/v1/rules/{id}/revision-submissions
Authorization: Bearer <RULE_SUBMISSION_WRITE_TOKEN>
```

The route is registered only when the existing controlled submission transport is enabled.

It reuses:

- the independent submission-write credential,
- the existing submission write abuse/rate boundary,
- strict bounded JSON parsing,
- authorization-before-protected-handler behavior.

A conceptual request is:

```json
{
  "base_version": 3,
  "name": "...",
  "description": "...",
  "category_code": "...",
  "rule_type": "keyword",
  "pattern": "...",
  "weight": 30,
  "severity": "high",
  "enabled": true,
  "explanation": "...",
  "recommendation": "..."
}
```

The client does **not** send:

```text
code
kind
target_risk_rule_id
source_submission_id
draft_digest
request_digest
actor metadata
publication metadata
```

The server obtains the target ID from the path and copies the current target code into the persisted submission snapshot.

This route creates only a pending non-executable proposal. It does not alter current Rule state.

## 11. Revision validation

Keep existing create validation strict, including duplicate-code rejection.

Do not weaken global `ValidateDraft` duplicate-code behavior merely to make revision publication pass.

Introduce contextual revision validation that shares common field/category/type/severity/regex/weight validation but applies these additional rules:

1. target Rule exists,
2. `base_version > 0`,
3. target current version equals `base_version`,
4. stored submission code equals target current code,
5. the proposed canonical snapshot differs from the current base snapshot,
6. all ordinary Rule field validation passes,
7. duplicate-code validation allows the target's own unchanged code but no different Rule with that code.

Expected stable validation/conflict concepts include:

```text
rule_not_found
stale_base_version
code_immutable
no_changes
category_not_found
unsupported_rule_type
unsupported_severity
invalid_regex
weight_out_of_range
```

Exact HTTP error-envelope naming remains consistent with existing repository conventions.

### Optional no-write revision validator

A later bounded implementation may expose a public no-write endpoint such as:

```text
POST /api/v1/rules/{id}/revisions/validate
```

but it is not required before the controlled revision-submission service works. If added, it must have zero persistence side effects and must not become a mutation credential bypass.

## 12. Review behavior for revision submissions

The review route remains:

```text
POST /api/v1/rule-submissions/{id}/reviews
```

with the existing independent review credential and server-owned actor label.

Review dispatches validation by stored submission `kind`:

### create

Use existing create-draft semantics.

### revision

Inside the review transaction/revalidation path:

- verify stored target/base shape,
- load the current target Rule,
- require current version equals stored `base_version`,
- require code equality,
- reject no-op snapshot,
- run contextual revision validation,
- preserve the existing terminal review state machine and review event/digest binding.

A stale revision is not approved by silently rebasing it onto a newer Rule version.

The maintainer must create/review a new revision submission against the new base.

## 13. Publication behavior

The publication route remains:

```text
POST /api/v1/rule-submissions/{id}/publications
```

with the existing independent publication credential and server-owned publisher actor label.

The service dispatches by stored submission `kind`.

### 13.1 New Rule publication

For `kind=create`, first publication atomically creates:

1. `RiskRule` current projection with `version=1`,
2. one `RuleSubmissionPublicationEvent`,
3. one `RiskRuleVersion` v1 snapshot with `source_kind=controlled_publication` linked to the source/review/publication IDs.

If any step fails, none commit.

Existing `enabled=false` preservation remains mandatory; do not regress to GORM zero-value/default rewriting.

### 13.2 Revision publication transaction

For `kind=revision`, publication must use only the stored approved submission snapshot.

Conceptual transaction:

```text
load approved submission
-> resolve exact replay if its publication event already exists
-> load/verify approved review + draft digest
-> load current target RiskRule
-> require target.version == submission.base_version
-> require target.code == submission.code
-> contextual revision revalidation
-> reject no-op
-> next_version = base_version + 1
-> append RiskRuleVersion(next_version, approved snapshot, controlled provenance)
-> conditional update current RiskRule WHERE id=? AND version=base_version
   to approved snapshot + version=next_version
-> require exactly one row updated
-> append one RuleSubmissionPublicationEvent for this submission/review/Rule
-> commit
```

The implementation may order the version/event inserts differently inside the transaction to obtain generated IDs, provided all three logical effects are atomic and rollback together.

Use explicit maps/column updates where needed so `enabled=false` is not lost to ORM zero-value behavior.

### 13.3 Source submission provenance

Revision publication does **not** change:

```text
RiskRule.source_submission_id
```

That field continues to mean original controlled creation provenance.

The current version's source is obtained from its `RiskRuleVersion` row.

## 14. Concurrency authority

### 14.1 Conditional current-version update

The final current-projection mutation must be conditional on the base version, conceptually:

```sql
UPDATE risk_rules
SET ..., version = :base_version + 1
WHERE id = :id
  AND version = :base_version;
```

Correctness requires:

```text
RowsAffected == 1
```

If zero rows are updated, the publication is stale/conflicted and its transaction must not commit a version/event.

### 14.2 Unique history key

The database unique constraint on:

```text
(risk_rule_id, version)
```

is an additional final authority against two committed rows claiming the same logical version.

### 14.3 Same-base competing revisions

For two different approved revision submissions targeting Rule 42 base version 3:

```text
submission A -> wants v4
submission B -> wants v4
```

exactly one may commit v4/current projection/publication event.

The loser returns a stale/conflict outcome with zero committed version/event/current mutation.

### 14.4 In-memory mutexes

Do not use an application-process mutex as the correctness boundary. Tests must prove behavior against PostgreSQL transactions/constraints.

## 15. Publication replay after newer versions exist

Existing first-publication replay logic currently assumes the current `RiskRule` still corresponds to the source publication.

That assumption must be replaced before revision publication is enabled.

For any submission with an existing publication event:

1. verify the event belongs to the same submission/review/digest and trusted actor contract,
2. resolve the exact `RiskRuleVersion` linked to that publication event,
3. verify that historical version snapshot matches the approved submission snapshot,
4. return replay for that exact published version,
5. do **not** require the current Rule to still equal that version.

Example:

```text
submission S1 -> v1
submission S2 -> v2
submission S3 -> v3
retry publication of S2
```

must resolve as replay of **v2**, not create v4, not fail merely because current is v3, and not return v3 as if it were S2's published snapshot.

A retry by a different trusted publisher actor remains a conflict under the existing publication actor-label contract.

## 16. Enable, disable, retirement, and deletion

`enabled` is versioned content.

A controlled disable operation is represented as:

```text
revision of current Rule with enabled=false
```

and therefore receives a new version and full provenance.

Re-enabling is another revision.

This slice does not introduce:

- direct toggle API,
- direct delete API,
- separate hidden activation state,
- irreversible `retired` status,
- automatic deletion of old versions.

For now, "retire" operationally means publish an `enabled=false` revision and leave history intact.

A future permanent-retirement or deletion policy requires separate Design First work.

## 17. Public history read contract

Additive read-only history is safe to expose separately from mutation credentials.

The first history endpoint should be conceptually:

```text
GET /api/v1/rules/{id}/history
```

It reads `RiskRuleVersion` by historical `risk_rule_id`, ordered newest-first by version.

It must not require the current `RiskRule` row to exist; this allows audit history to survive a legacy/out-of-band hard deletion.

First-slice response may include:

```text
risk_rule_id
current_exists
current_version (nullable when current missing)
versions[]:
  version
  code
  name
  description
  category_code
  rule_type
  pattern
  weight
  severity
  enabled
  explanation
  recommendation
  source_kind
  created_at
```

Do not expose in the first public history payload:

- Bearer credentials,
- trusted actor labels,
- raw access grants,
- internal publisher/reviewer identity assumptions,
- review reason text,
- hidden digests needed only for integrity checks.

`source_kind=legacy_baseline` is intentionally visible so consumers do not mistake an observed migration baseline for fully reconstructed history.

The normal current `GET /api/v1/rules/{id}` adds the current positive `version` field.

Per-rule visual changelog/diff presentation remains a separate Roadmap item after trustworthy persistence/API exists.

## 18. Deterministic analysis traceability

Rule history is incomplete if a new analysis cannot say which version it used.

Extend internal deterministic engine input/output conceptually:

```go
type riskengine.Rule struct {
    Version uint
    // existing fields...
}

type MatchedRule struct {
    RuleCode    string `json:"rule_code"`
    RuleVersion uint   `json:"rule_version"`
    // existing fields...
}
```

Persistence-backed analysis loads each enabled current `RiskRule.version` into the engine.

New `/analysis/text`, `/analysis/preview`, controlled assisted analysis, and authenticated browser assisted analysis therefore return deterministic matched rules with the exact Rule version used.

`AnalysisRecord.matched_rules` for newly persisted analyses naturally stores this additive field.

### 18.1 Historical AnalysisRecord policy

Do **not** rewrite existing `AnalysisRecord.matched_rules` JSON to add a guessed version.

Pre-versioning records did not store enough information to prove the exact version when mutable Rules may have existed.

Clients reading old JSON must tolerate a missing `rule_version` as historical unknown/pre-versioning data.

### 18.2 CLI static seed behavior

The CLI currently analyzes static seed JSON rather than database version rows.

Seed JSON must not be polluted with server-owned history metadata solely for this feature.

The CLI treats an unversioned static seed Rule as implicit baseline version `1` when constructing `riskengine.Rule` so new deterministic output remains positive and consistent.

This does not create database history for the CLI dataset.

## 19. Security and trust boundaries

The versioning feature must preserve all current trust separations.

### Public

Allowed without mutation credential:

- current Rule reads,
- history reads,
- existing no-write create validation,
- any future explicitly no-write revision validation.

### Submission writer

May create only non-executable create/revision proposals.

Cannot:

- approve,
- publish,
- choose trusted actor labels,
- modify current Rule directly,
- select a different Rule code for a revision,
- force a stale base through publication.

### Reviewer

May terminally approve/reject the exact stored snapshot through the independent review credential.

Cannot publish.

### Publisher

May publish only an approved, integrity-checked stored snapshot through the independent publication credential.

Cannot replace the approved fields with client-supplied content.

### AI

No authority to mutate version state, review state, publication state, Rule content, or current version.

## 20. Compatibility

### Existing create-submission clients

Existing create submission/review/publication flows remain supported.

Existing stored submissions are treated as `kind=create` after migration.

### Existing current Rule readers

The `version` field is additive.

### Existing analysis clients

`matched_rules[].rule_version` is additive for new responses.

### Existing analysis history

Old `AnalysisRecord` JSON remains unchanged.

### Seed data

No version/source fields are required in community seed JSON.

### Database

This work requires schema additions and preparation/backfill logic but no destructive rewrite of existing publication/review audit rows.

## 21. Implementation sequence

Do not implement the whole design in one large PR.

### I1 — Version persistence + honest baseline + analysis traceability

Bounded scope:

- `RiskRule.version` current projection field,
- `RiskRuleVersion` model/table,
- snapshot digest,
- idempotent `PrepareRiskRuleVersionHistory`,
- exact controlled/legacy baseline backfill rules,
- initial publication creates/links v1,
- publication replay resolves exact version rather than assuming current projection,
- deterministic matched-rule `rule_version`,
- CLI implicit seed version 1,
- focused SQLite + PostgreSQL upgrade/integrity tests.

No revision-submission mutation yet.

### I2 — Revision submission intent + idempotency + review

Bounded scope:

- submission `kind`, target/base/request digest,
- migration/backfill of existing submissions,
- pending request-digest unique index,
- protected revision-submission route/service,
- contextual revision validation,
- review-time stale-base/no-op/code-immutability enforcement,
- no executable Rule mutation yet.

### I3 — Revision publication + concurrency

Bounded scope:

- publication dispatch for revision submissions,
- atomic history append + conditional current projection update + event,
- exact historical replay,
- PostgreSQL same-base competing-revision acceptance,
- enabled false/true versioned transitions,
- zero-write negative-path assertions.

### I4 — Public history read API and documentation

Bounded scope:

- `GET /api/v1/rules/{id}/history`,
- safe public response DTO/redaction,
- current Rule version docs,
- API/data-schema/community-workflow updates,
- read-only tests including history surviving missing current Rule.

### Later — per-rule changelog presentation

Only after I1-I4 are stable:

- Vue history/changelog UI,
- adjacent-version diff presentation,
- UX for legacy-baseline disclosure,
- maintainer/community workflow UX as separately designed.

## 22. Required acceptance matrix

Implementation slices must preserve the repository's standard full CI plus focused versioning tests.

### Persistence/backfill

- fresh database: new Rules have positive version/history,
- seed/manual Rule -> one `legacy_baseline` v1,
- proven controlled publication unchanged -> controlled v1,
- proven controlled publication + pre-freeze current drift -> controlled v1 + legacy baseline v2,
- orphan historical publication -> historical version only, no Rule recreation,
- repeated preparation -> no duplicates/renumbering,
- inconsistent provenance/digest -> fail closed,
- current projection/history mismatch on later startup -> fail closed.

### Create publication

- one new Rule + one publication event + one v1 version atomically,
- explicit `enabled=false` preserved,
- event/version failure rolls back all effects,
- same-submission concurrency converges,
- same code across different creates still has one winner.

### Revision proposal/review

- exact target/base/draft retry replays one pending submission,
- same content on different target/base does not collide,
- stale base rejected,
- code cannot be changed,
- no-op rejected,
- structural/category/regex validation preserved,
- approval creates zero Rule/version/publication mutation.

### Revision publication

- base N -> exactly N+1,
- current projection equals N+1 snapshot after commit,
- old versions unchanged,
- `source_submission_id` remains original creation source,
- revision event/version carries new source provenance,
- two different revisions from same base -> exactly one winner,
- loser has zero committed Rule/history/event change,
- retry historical published revision after newer version -> exact historical replay,
- different publisher actor replay -> conflict,
- disabled revision remains disabled,
- failed validation/integrity/stale paths produce zero prohibited writes.

### Analysis traceability

- deterministic API matched Rule includes current positive version,
- persisted new `AnalysisRecord.matched_rules` contains that version,
- preview remains no-write,
- assisted/browser deterministic authority unchanged,
- old history rows are not mass-rewritten,
- CLI static seed analysis emits version 1.

### HTTP/security

- anonymous direct Rule mutation remains 404,
- revision submission route is absent when controlled submissions disabled,
- wrong/missing submission credential produces zero submission/current/history writes,
- review/publication credential separation remains enforced,
- unknown JSON fields and oversize bodies remain rejected,
- history GET is side-effect free,
- public history response excludes trusted actor labels and secrets.

### CI

Every implementation PR must pass actual GitHub Actions:

- Backend Test,
- Backend Race,
- Backend Vet,
- PostgreSQL 16 / Redis 7 integration paths,
- Frontend Install,
- Browser Assisted Safety Test,
- Frontend Typecheck,
- Frontend Build,
- Extension Test.

## 23. STOP conditions

Stop implementation and return to design if any slice would require one of the following:

- reintroducing anonymous direct Rule mutation,
- silently changing Rule code during a controlled revision,
- rebasing an approved stale revision without new review,
- rewriting historical review/publication events,
- guessing historical Rule versions for existing AnalysisRecords,
- making process-local locking the concurrency authority,
- weakening create duplicate-code validation globally,
- exposing trusted mutation credentials/actor labels in public history,
- letting AI become a Rule mutation or approval authority,
- silently changing `RiskRule.source_submission_id` from original-source semantics,
- destructive deletion/renumbering of existing history to make migration easier.

## 24. Non-goals

This design does not claim completion of:

- contributor accounts/OAuth/general RBAC,
- public anonymous Rule writes,
- AI rule approval,
- cryptographic append-only audit,
- permanent Rule deletion governance,
- an irreversible retirement state,
- full Vue changelog/history presentation,
- rollback-by-version as an automatic operation,
- editing an already-approved submission,
- arbitrary branching/merging of Rule histories.

A rollback, if later needed, should itself be a newly reviewed revision whose content intentionally matches an older snapshot; history must move forward rather than deleting later versions.