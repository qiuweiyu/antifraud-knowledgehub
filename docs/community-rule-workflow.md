# Community Rule Workflow

AntiFraud-KnowledgeHub supports a human-reviewed workflow for proposing new explainable anti-fraud rules while keeping executable `RiskRule` records separate from unreviewed contributions.

This document describes the **current implemented workflow on `main`**. It is intentionally conservative: community contributors do not receive anonymous/public mutation credentials, and AI is not a final approval authority.

## Two contribution paths

### 1. Normal open-source contribution path

For ordinary community contributors, the recommended path remains a scoped GitHub pull request:

1. Follow [Rule Contribution Format](rule-contribution-format.md).
2. Use synthetic or fully anonymized examples only.
3. Validate the draft with the Rules UI or `POST /api/v1/rules/validate`.
4. Include expected matches, false-positive considerations and test evidence where practical.
5. Open a scoped pull request for human maintainer review.

No repository-controlled bearer credential needs to be shared with external contributors for this path.

### 2. Controlled maintainer transport

Deployments that intentionally enable the controlled workflow can use three independently configured, default-off HTTP transports:

```text
valid rule draft
    |
    v
POST /api/v1/rule-submissions
    |
    | pending RuleSubmission only
    v
POST /api/v1/rule-submissions/{id}/reviews
    |
    | approved or rejected + one review audit event
    v
POST /api/v1/rule-submissions/{id}/publications
    |
    | approved sources only
    v
RiskRule + one publication audit event
```

The three mutation credentials are deliberately separate:

```text
RULE_SUBMISSION_WRITE_TOKEN
RULE_SUBMISSION_REVIEW_TOKEN
RULE_SUBMISSION_PUBLICATION_TOKEN
```

Review and publication also use server-owned operational attribution labels:

```text
RULE_SUBMISSION_REVIEW_ACTOR_LABEL
RULE_SUBMISSION_PUBLICATION_ACTOR_LABEL
```

These labels are audit attribution, not proof of a specific person's cryptographic identity. The current transport is not OAuth or a general RBAC/user-account system.

## Default-off security model

All controlled mutation transports are disabled by default.

```text
RULE_SUBMISSIONS_ENABLED=false
RULE_SUBMISSION_REVIEWS_ENABLED=false
RULE_SUBMISSION_PUBLICATIONS_ENABLED=false
```

A disabled route is not registered and therefore receives the router's normal `404` behavior.

When enabled, configuration validation fails closed if the required credential or trusted actor label is invalid. Review and publication credentials must be independent from the submission-write credential, and the publication credential must also be independent from the review credential.

Do not publish these credentials in examples, issues, screenshots, logs, release notes or client-side code.

## Step 1: validate a draft

Validation is available before persistence:

```text
POST /api/v1/rules/validate
```

Validation checks the current rule contract, including required fields, category existence, duplicate codes, supported rule types/severities, weight bounds and regular-expression compilation.

Validation performs no rule creation. A valid draft is **not approved** merely because it passes structural checks.

## Step 2: create or replay a pending submission

When the controlled submission transport is enabled:

```text
POST /api/v1/rule-submissions
Authorization: Bearer <RULE_SUBMISSION_WRITE_TOKEN>
Content-Type: application/json
```

The request body uses the existing rule draft contract.

A new valid canonical draft creates one `RuleSubmission` with server-owned status:

```text
pending
```

An exact canonical retry replays the same pending submission rather than creating a duplicate. Pending submissions are deliberately separate from executable `RiskRule` records and therefore cannot enter the risk engine.

The submission transport has Redis-backed abuse controls. If the limiter cannot evaluate safely, the write path fails closed rather than bypassing protection.

## Step 3: human review

When controlled review is enabled:

```text
POST /api/v1/rule-submissions/{id}/reviews
Authorization: Bearer <RULE_SUBMISSION_REVIEW_TOKEN>
Content-Type: application/json
```

Example synthetic approval command:

```json
{
  "decision": "approved",
  "reason": "The synthetic signal is specific, explainable and has acceptable false-positive risk."
}
```

The only terminal review decisions are:

```text
approved
rejected
```

A review records the terminal decision and its audit event in one database transaction. Exact retry returns the same audit event; a different second terminal command is a conflict.

Approval re-runs the current draft validator. If the stored draft is no longer valid, approval fails rather than grandfathering an invalid rule.

**Approval does not publish a rule.** Successful review creates or updates no `RiskRule`.

Review reasons must not contain victim personal data, credentials, bank-card information, private chat dumps, access tokens or live malicious URLs.

## Step 4: controlled publication

Only an approved submission backed by its matching approved review event can be published.

When controlled publication is enabled:

```text
POST /api/v1/rule-submissions/{id}/publications
Authorization: Bearer <RULE_SUBMISSION_PUBLICATION_TOKEN>
Content-Type: application/json
```

The only valid client body is:

```json
{}
```

The client cannot provide or override:

- rule snapshot fields,
- `enabled`, code, pattern, severity or weight,
- actor metadata,
- review metadata,
- digest/provenance fields,
- `force`, `override` or `recreate` flags.

The server publishes the already-approved stored snapshot. Before first publication it verifies approved review provenance/digest integrity and re-runs current validation.

A successful first publication atomically creates:

1. one executable `RiskRule`, and
2. one `RuleSubmissionPublicationEvent`.

The publication event records source/review/rule identifiers, the frozen rule code, server-owned publisher attribution, source digest and creation time. It is publication audit provenance; it is not a cryptographic append-only ledger.

The submission remains `approved`; publication does not invent a `published` submission status or rewrite the human review decision/reason.

## Idempotency and concurrency

PostgreSQL constraints and transactions are the concurrency authority for review/publication correctness.

The workflow has automated coverage for concurrent operations, including:

- identical pending submission creation converging on one pending submission,
- concurrent identical review converging on one terminal event,
- approve-vs-reject contention producing one terminal winner,
- concurrent same-submission publication producing one `RiskRule` and one publication event,
- two approved submissions racing for the same rule code producing one rule winner.

Application-process mutexes are not used as the publication correctness authority.

## Publication replay and later rule lifecycle

An exact same-publisher retry returns the existing publication identity instead of creating duplicates.

The publication event records what was initially published. Later normal `RiskRule` updates or toggles do not rewrite that historical publication event.

Current `RiskRule` behavior permits hard deletion. If a publication-created rule is later hard-deleted, the publication event remains, and retrying publication returns a conflict. It **does not recreate the deleted rule**.

This means publication audit provenance and the mutable current rule lifecycle are deliberately different concepts.

## HTTP security boundaries

The controlled transports use strict Bearer authorization and bounded JSON parsing.

Key rules include:

- authorization occurs before protected review/publication handler parsing,
- write, review and publication credentials are not interchangeable,
- unknown JSON fields are rejected,
- request bodies have explicit size limits,
- path submission IDs use strict positive decimal parsing,
- credentials are never returned in error responses,
- failed review/publication transport paths are tested for zero prohibited writes.

Publication does not add a Redis limiter in its first controlled transport. It is default-off, uses an independent credential and relies on database constraints/transactions for mutation correctness. A future publication-specific limiter requires its own bounded design if exposure or automation volume justifies it.

## What is not implemented

The current workflow should not be described as any of the following:

- anonymous public rule submission,
- contributor accounts,
- OAuth/RBAC reviewer identity,
- cryptographic non-repudiation,
- AI auto-approval,
- a public maintainer review dashboard,
- rule versioning/history,
- a complete per-rule changelog.

Those capabilities require separate design and implementation work.

## Maintainer checklist

Before approving or publishing a contribution, confirm at minimum:

- [ ] The draft passes current validation.
- [ ] The signal is specific enough to avoid obvious false positives.
- [ ] Category, rule type, weight and severity are appropriate.
- [ ] Explanation and recommendation are understandable and safe.
- [ ] Examples are synthetic or fully anonymized.
- [ ] No secrets, credentials or victim personal data are present.
- [ ] Relevant tests or safe examples cover the intended behavior when practical.
- [ ] Review uses the review credential, not the write credential.
- [ ] Publication uses the independent publication credential.
- [ ] Publication is performed only after an approved terminal review.

See [Rule Contribution Format](rule-contribution-format.md) for field-level guidance and [API](api.md) for the current transport contract.

## CI acceptance

Repository changes that affect this workflow remain subject to the normal GitHub Actions gates:

```text
Backend: go test ./..., configured race tests, go vet ./...
Services: PostgreSQL 16, Redis 7
Frontend: install, typecheck, build
```

A red CI run must not be merged merely because the change appears documentation- or transport-only.
