# Minimal Rule Submission Persistence Design

This document freezes the smallest persistence boundary for future community rule submissions in AntiFraud-KnowledgeHub.

It is a design-only step for Issue #10. No runtime implementation, migration, public submission endpoint, authentication, reviewer role, approval transition, versioning, bot, or AI review is introduced by this document.

## Decision summary

The first persisted community-submission object will be a **non-executable snapshot of a validated rule draft**.

Key decisions:

1. A submission is not a `RiskRule` and must never participate in risk analysis.
2. The initial and only supported state is `pending`.
3. The existing rule validator must pass before a submission can be inserted.
4. Draft fields are immutable after insertion in the first implementation slice.
5. There is no foreign key to `RiskRule` at submission time.
6. There is no `user_id`, reviewer ID, submitter account, approval record, or role model in this phase because the repository currently has no authentication/identity domain.
7. The first persistence implementation should not expose a new unauthenticated public write endpoint. Public submission transport, abuse controls, authentication, or rate limiting must be designed separately.
8. A future approval path must revalidate the stored draft before creating a real `RiskRule`.

## Why a separate submission record is required

`RiskRule` is production data consumed by the explainable risk engine. Community input must not become active simply because it was persisted.

A separate submission record provides a safety boundary:

`untrusted draft -> validation -> pending submission -> human review -> future approval step -> RiskRule`

The current project only implements the first validation step. This design defines the next storage boundary without pretending that the later review workflow already exists.

## Proposed model

The first implementation should add a model conceptually equivalent to:

```go
type RuleSubmission struct {
    ID             uint      `json:"id" gorm:"primaryKey"`
    Status         string    `json:"status" gorm:"index;size:32;not null;default:pending"`
    Code           string    `json:"code" gorm:"index;size:120;not null"`
    Name           string    `json:"name" gorm:"size:180;not null"`
    Description    string    `json:"description"`
    CategoryCode   string    `json:"category_code" gorm:"index;size:100;not null"`
    RuleType       string    `json:"rule_type" gorm:"size:40;not null"`
    Pattern        string    `json:"pattern" gorm:"not null"`
    Weight         int       `json:"weight" gorm:"not null"`
    Severity       string    `json:"severity" gorm:"size:40;not null"`
    Enabled        bool      `json:"enabled" gorm:"not null;default:true"`
    Explanation    string    `json:"explanation"`
    Recommendation string    `json:"recommendation"`
    CreatedAt      time.Time `json:"created_at" gorm:"index"`
    UpdatedAt      time.Time `json:"updated_at"`
}
```

The exact Go file and GORM tags may be adjusted during implementation, but the semantic boundary above is frozen for the first slice.

## Field ownership

### Submission-owned fields

The submission owns a snapshot of the same rule-draft content already accepted by `DraftRequest`:

- `code`
- `name`
- `description`
- `category_code`
- `rule_type`
- `pattern`
- `weight`
- `severity`
- `enabled`
- `explanation`
- `recommendation`

These fields describe what was proposed at submission time.

They are **not references to mutable `RiskRule` fields** and must not be silently rewritten if the production rule set changes later.

### System-owned fields

The persistence layer owns:

- `id`
- `status`
- `created_at`
- `updated_at`

Clients must not be allowed to choose the initial status. The service sets it to `pending`.

### Identity fields intentionally omitted

Do not add any of the following in the first slice:

- `user_id`
- `reviewer_id`
- `approved_by`
- `rejected_by`
- email / phone ownership
- account FK
- organization FK

The repository has no identity model today. Adding placeholder identity columns would create a false contract that later authentication work would have to preserve.

If identity is added in the future, it should be introduced by a dedicated design and migration.

## Status model

The first implementation supports exactly one state:

`pending`

Do not implement:

- `approved`
- `rejected`
- `changes_requested`
- `withdrawn`
- `published`

A one-state model is intentional. It allows storage and review tooling to be built without prematurely freezing transition semantics.

When review transitions are designed later, they should be explicit and audited rather than arbitrary updates to the row.

## Relationship to `RiskRule`

There is **no foreign key from `RuleSubmission` to `RiskRule` in the first phase**.

Reasons:

1. A pending submission may never become a rule.
2. A submission code can become stale if another rule is created before review.
3. The approved rule may require maintainer edits before publication.
4. Pre-allocating a nullable `risk_rule_id` now would freeze approval semantics that have not been designed.

Future approval should follow this order:

1. Load the immutable pending submission.
2. Re-run current validation against the current repository/database state.
3. Perform human review.
4. Create a new `RiskRule` only after approval.
5. Record the publication relationship in a later review/audit design, not by mutating the original draft fields.

## Validation-before-persistence contract

The existing `rule.ValidateDraft` remains the single source of truth for structural rule validation.

The future create-pending service must:

1. Bind/map input to the existing `DraftRequest` contract.
2. Call `ValidateDraft` using the current database.
3. If `valid == false`, return the structured validation result and perform **zero writes**.
4. If valid, map the normalized draft fields into a new `RuleSubmission`.
5. Force `status = pending` in server-side code.
6. Insert exactly one submission row.

Do not duplicate the validator rules inside a new submission handler or repository.

### Revalidation at approval time

Validation at submission time is not permanent approval.

A future approval step must run validation again because, after submission:

- a `RiskRule` with the same code may have been created,
- categories may have changed,
- supported rule types or severity constraints may change,
- new safety checks may be added.

## Duplicate handling

`RuleSubmission.code` should be indexed but **not unique** in the first slice.

The existing validator already prevents collision with a currently persisted `RiskRule`, but two pending drafts may legitimately represent competing revisions or repeated proposals.

Do not introduce a global unique constraint on submission code before review/version semantics are designed.

Exact replay/idempotency protection is deferred. If a public write API is added later, a canonical draft digest or explicit idempotency key can be designed together with abuse controls.

## Immutability

In the first implementation:

- there is a create operation for pending submissions,
- there is no update operation for draft content,
- there is no delete operation exposed to clients,
- there are no state transitions.

If a contributor needs to revise a draft before review workflow exists, the safe model is to create a new pending submission rather than mutate the historical snapshot.

Future review events should preserve who changed what and why instead of overwriting submitted evidence.

## Data-safety boundary

The persisted object should contain only rule-draft fields needed for review.

Do not add free-form evidence blobs, attachments, chat transcripts, victim profiles, screenshots, or credentials to the first schema.

The repository contribution policy continues to prohibit real:

- victim names,
- phone numbers used as personal evidence,
- bank-card numbers,
- ID numbers,
- private chat identifiers,
- passwords,
- API keys,
- access tokens,
- unredacted screenshots.

A special caveat applies to anti-fraud rules: patterns may intentionally describe **formats** such as phone-number or bank-card regular expressions. Therefore a naive PII detector must not reject every numeric-looking pattern. Data safety still requires human review and safe synthetic examples.

## Database/index guidance

The minimal useful indexes are:

- primary key on `id`,
- index on `status`,
- index on `category_code`,
- index on `code`,
- index on `created_at`.

Do not add a unique index on `code`.

Do not add a foreign key to `RiskRule` in the first slice.

The repository currently uses GORM `AutoMigrate`; the first implementation should follow that existing project convention rather than introduce a new migration framework only for this feature.

## Public API boundary

The first persistence implementation should **not** automatically register an unauthenticated internet-facing endpoint such as:

`POST /api/v1/rule-submissions`

The project currently has no identity, authorization, anti-spam, quota, or rate-limit contract for community writes. Persisting arbitrary public input before those boundaries are decided would create an avoidable abuse surface.

The next implementation slice should therefore be limited to:

- `RuleSubmission` database model,
- `AutoMigrate` registration,
- a small repository/service function that creates a validated pending submission,
- tests proving validation-before-write and immutability expectations.

A network-facing submission endpoint should be a separate later task with an explicit abuse-control decision.

## First implementation acceptance criteria

The next code PR should be considered complete only if all of the following are true:

1. `RuleSubmission` is migrated by the existing database startup path.
2. Only `pending` submissions can be created by the new service.
3. The service reuses `ValidateDraft` rather than reimplementing validation.
4. Invalid drafts produce no submission row.
5. Valid drafts produce one row containing a snapshot of the proposed rule fields.
6. Creating a submission does not create or modify any `RiskRule`.
7. No update/delete/state-transition API is introduced.
8. No user/reviewer/auth fields are invented.
9. SQLite-backed tests pass because SQLite remains the lightweight local development path.
10. Existing PostgreSQL-compatible GORM tags remain portable.
11. Existing backend/frontend CI remains green.

## Explicit non-goals for the next implementation

The next implementation must not add:

- public unauthenticated submission routes,
- authentication or users,
- reviewer roles,
- approve/reject transitions,
- `RiskRule` publication,
- rule versioning,
- review comments/history tables,
- email notifications,
- GitHub bots,
- AI review,
- browser extension integration.

These are later bounded steps.

## Follow-up sequence

After this design, the recommended sequence is:

1. **Minimal persistence implementation** — model + validation-backed create service + tests.
2. **Read-only maintainer inspection** — list/get pending submissions without approval actions.
3. **Submission transport / abuse-control design** — decide authentication, rate limiting, or other write-boundary controls before exposing public writes.
4. **Human review state design** — only then define approve/reject/change-requested semantics and audit records.
5. **Publication path** — revalidate and create `RiskRule` after explicit approval.

This keeps Issue #1 incremental and prevents community input from bypassing explainability and human-review guarantees.
