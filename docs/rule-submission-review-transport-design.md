# Controlled Rule Submission Review HTTP Transport Design

This document freezes the first bounded HTTP transport contract for controlled maintainer review of `RuleSubmission`.

It is a **design-only** step for Issue #43. It does not register a review route, add a handler, add a Redis review limiter, change database schema, add reviewer identity/RBAC, add frontend review UI, or publish any `RiskRule`.

For transport details, this document supersedes the earlier **future/suggested HTTP semantics** section in `rule-submission-review-audit-design.md`. It does not change that document's frozen review state machine, audit, transaction, replay, revalidation, actor-attribution, or reason semantics.

Progress toward #1.

## Existing frozen dependencies

The implementation must reuse the current repository contracts rather than invent parallel behavior:

- `RULE_SUBMISSION_REVIEWS_ENABLED` is default-off.
- `RULE_SUBMISSION_REVIEW_TOKEN` is independent from `RULE_SUBMISSION_WRITE_TOKEN`.
- `RULE_SUBMISSION_REVIEW_ACTOR_LABEL` is server-owned, trimmed, required when reviews are enabled, and limited to 120 UTF-8 bytes.
- `SubmissionReviewAuthorization` is the only review Bearer-token authorization middleware for this slice.
- `ReviewPendingSubmission` is the only review mutation service for this slice.
- Review decisions are `approved` or `rejected` after service normalization.
- Review reason is trimmed, required, and limited to 2,000 UTF-8 bytes.
- `actor_kind` is fixed by the service to `controlled_maintainer`.
- Approval revalidates the stored submission snapshot.
- Approval does not publish or mutate a `RiskRule`.
- Exact review replay returns the already-created event; a different terminal command is a conflict.
- State transition and review-event append remain one PostgreSQL transaction.
- The project response envelope remains `response.Envelope`.

## Route

Freeze the first controlled mutation route as:

```text
POST /api/v1/rule-submissions/:id/reviews
```

This matches the repository's existing Gin `:id` route style.

The route is registered **only** when `RULE_SUBMISSION_REVIEWS_ENABLED=true` and configuration validation has succeeded.

When reviews are disabled, the route is not registered. Requests therefore receive the router's normal `404` behavior. The disabled path must perform zero submission mutations, zero review-event writes, and zero `RiskRule` writes.

## Security and processing order

Freeze the processing order as:

```text
Review feature gate / route registration
-> SubmissionReviewAuthorization(review token)
-> Content-Type validation
-> bounded request body
-> strict single-object JSON decoding
-> path submission ID parsing
-> server-owned actor injection
-> ReviewPendingSubmission(...)
-> response mapping
```

Authorization intentionally runs before the handler parses the body or submission ID. An unauthorized caller must not be able to use review transport to probe submission existence or validation state.

There is **no review Redis rate limiter in this slice**. The existing submission-write limiter must not be copied or reused implicitly. If review-specific abuse control becomes necessary, it requires a separate bounded design decision.

## Authorization

The route must use:

```go
middleware.SubmissionReviewAuthorization(cfg.RuleSubmissionReviewToken)
```

Do not accept the submission-write token for review.

Existing middleware behavior remains authoritative:

- missing or malformed Bearer header -> `401`,
- wrong token -> `401`,
- blank expected token -> fail closed with `401`,
- correct independent review token -> handler may execute,
- error response code remains `unauthorized`,
- token material must never be reflected in responses or logs.

The handler must not add a second token parser or comparison implementation.

## Request media type

The endpoint accepts only a valid media type whose parsed base type is:

```text
application/json
```

Syntactically valid media-type parameters such as `charset=utf-8` are allowed because the repository already uses `mime.ParseMediaType` for strict submission transport.

Missing, malformed, or non-JSON content type returns:

```text
415 Unsupported Media Type
code: unsupported_media_type
message: rule submission reviews require application/json
```

This failure occurs before body decoding and must cause zero database writes.

## Request body boundary

Freeze the maximum raw HTTP request body at **16 KiB**.

Rationale:

- the client may send only `decision` and `reason`,
- the normalized reason is already bounded to 2,000 UTF-8 bytes,
- JSON escaping can expand the raw wire representation substantially,
- 16 KiB leaves safe room for worst-case escaping while still rejecting accidental or hostile large bodies.

The implementation should use `http.MaxBytesReader` before JSON decoding.

A body exceeding the limit returns:

```text
413 Request Entity Too Large
code: review_body_too_large
message: rule submission review body exceeds 16 KiB
```

No partial body processing may produce a review mutation.

## Client JSON contract

The only accepted client object is conceptually:

```json
{
  "decision": "approved",
  "reason": "Matches the documented fraud pattern and has an acceptable false-positive profile."
}
```

The request DTO must contain exactly these supported fields:

- `decision`
- `reason`

The handler must not bind directly to `database.RuleSubmission`, `database.RuleSubmissionReviewEvent`, or `SubmissionReviewCommand` if that would make server-owned fields client-bindable.

The client must never control:

- `actor_kind`,
- `actor_label`,
- `from_status`,
- `to_status`,
- current submission status,
- `draft_digest`,
- review event ID,
- reviewer user ID,
- timestamps,
- publication flags,
- any `RiskRule` field.

## Strict JSON semantics

Use `encoding/json.Decoder` with unknown-field rejection.

Freeze these rules:

- body must contain exactly one JSON object,
- empty body is invalid,
- malformed JSON is invalid,
- unknown fields are invalid,
- a second/trailing JSON value is invalid,
- trailing JSON whitespace is allowed,
- JSON `null` is invalid because it is not a review object,
- arrays and scalar JSON values are invalid.

All decode/shape failures return:

```text
400 Bad Request
code: invalid_review_json
message: request body must be a single valid JSON object using supported review fields
```

Decode failures must occur before `ReviewPendingSubmission` is called.

## Decision and reason semantics

Do not duplicate the service's business validation in the HTTP handler.

The handler passes the decoded `decision` and `reason` to `ReviewPendingSubmission`; service normalization remains authoritative:

- leading/trailing whitespace is trimmed,
- normalized decision must be exactly `approved` or `rejected`,
- normalized reason must be non-empty,
- normalized reason must be at most 2,000 UTF-8 bytes.

`ErrInvalidSubmissionReview` maps to:

```text
400 Bad Request
code: invalid_submission_review
message: invalid rule submission review
```

The response must not echo rejected raw reason text.

## Submission ID parsing

The Gin path parameter `:id` must be parsed by the handler as a strict positive base-10 unsigned integer.

Accepted form:

```text
1
42
18446744073709551615   # only if representable by the implementation's uint target
```

Rejected forms include:

- empty value,
- `0`,
- negative values,
- leading `+`,
- decimal points,
- hexadecimal notation,
- surrounding whitespace,
- non-digits,
- overflow for the target `uint`.

Invalid ID returns:

```text
400 Bad Request
code: invalid_submission_id
message: rule submission id must be a positive decimal integer
```

This failure must occur before the review service is called.

## Server-owned actor attribution

The client body never supplies actor information.

The handler constructs the service command from:

```text
decision    <- decoded request JSON
reason      <- decoded request JSON
actor_label <- cfg.RuleSubmissionReviewActorLabel
actor_kind  <- fixed inside ReviewPendingSubmission as controlled_maintainer
```

`RULE_SUBMISSION_REVIEW_ACTOR_LABEL` remains an operational attribution label, not proof of a person's authenticated identity.

Do not describe this transport as OAuth, RBAC, reviewer-user authentication, non-repudiation, or cryptographic identity.

## Service error mapping

The handler must map the existing service/database result classes deterministically.

### Missing submission

`gorm.ErrRecordNotFound` from loading the requested submission maps to:

```text
404 Not Found
code: submission_not_found
message: rule submission not found
```

Do not distinguish authorization-valid nonexistent IDs in any more detail.

### Invalid review command

`ErrInvalidSubmissionReview` maps to:

```text
400 Bad Request
code: invalid_submission_review
message: invalid rule submission review
```

### Existing different terminal review

`ErrSubmissionReviewConflict` maps to:

```text
409 Conflict
code: submission_review_conflict
message: rule submission already has a different terminal review
```

### Approval-time current validation failure

`ErrSubmissionApprovalValidation` maps to:

```text
409 Conflict
code: submission_approval_validation_failed
message: rule submission is no longer valid for approval
```

This first transport slice does not extend the common API error envelope with validation-detail payloads. The response must not echo stored pattern, description, explanation, recommendation, draft digest, or raw database errors.

### Review integrity failure

`ErrSubmissionReviewIntegrity` maps to:

```text
500 Internal Server Error
code: submission_review_integrity_error
message: rule submission review integrity check failed
```

Integrity failures must not be converted into `409` merely to make the API appear recoverable.

### Other database/service failures

Any other unexpected service/database error maps to:

```text
500 Internal Server Error
code: submission_review_failed
message: rule submission review could not be completed
```

No internal SQL, DSN, table name, token, raw review reason, or raw GORM error is returned to the client.

## Success and exact replay response

Both a newly committed terminal review and an exact review replay return:

```text
200 OK
```

The handler must return an explicit transport DTO rather than serialize GORM persistence structs directly.

Freeze the successful `data` shape as:

```json
{
  "submission_id": 123,
  "status": "approved",
  "review_event_id": 456,
  "decision": "approved",
  "actor_kind": "controlled_maintainer",
  "actor_label": "maintainer-console",
  "created_at": "2026-08-18T11:30:00Z",
  "replay": false
}
```

Fields:

- `submission_id` comes from the stored submission/event relationship,
- `status` is the committed terminal submission status,
- `review_event_id` identifies the one terminal audit event,
- `decision` is the stored event decision,
- `actor_kind` and `actor_label` are stored audit attribution,
- `created_at` is the stored review-event timestamp encoded using Go's normal JSON time representation,
- `replay` is `false` for the transaction winner and `true` for an exact replay.

Do **not** include in this first mutation response:

- the raw review reason,
- the draft snapshot,
- `draft_digest`,
- validation internals,
- the review token,
- publication state.

The event remains queryable through internal review-read functions; a future maintainer inspection API is a separate bounded task.

## No `RiskRule` side effect

Every transport path, including successful approval, must preserve:

```text
RiskRule create count = 0
RiskRule update count = 0
RiskRule enable/toggle count = 0
```

Approval means accepted for a future publication step only.

The handler must call `ReviewPendingSubmission`; it must not call current `RiskRule` create/update handlers or write `RiskRule` directly.

## Zero-write failure boundary

The future implementation tests must prove that these failures create no terminal status mutation and no review event:

- feature disabled,
- missing review token,
- malformed review token,
- wrong review token,
- submission-write token used as review token,
- unsupported/malformed content type,
- oversized body,
- empty body,
- malformed JSON,
- unknown JSON field,
- trailing JSON value,
- non-object JSON,
- invalid submission ID,
- invalid decision,
- empty/invalid/oversized reason,
- missing submission,
- approval revalidation failure.

`RiskRule` writes must remain zero for every case above and for successful review paths.

## Logging and data safety

Request logging remains metadata-only.

Allowed review metadata may include:

- HTTP route/status/latency,
- parsed submission ID after authorization,
- target decision after successful decode,
- created/replay/conflict outcome,
- stable validation field/error codes if later logged by service code,
- actor label only if operators continue to classify it as non-secret.

Do not log by default:

- Authorization header,
- review token,
- submission-write token,
- raw request body,
- review reason,
- rule pattern or free text,
- canonical draft JSON,
- draft digest,
- database DSN or SQL containing user material.

The endpoint is not an evidence-upload channel. Review reasons must not contain victim PII, credentials, bank-card data, private chat dumps, access tokens, or live malicious URLs.

## Required implementation test matrix

The future code PR is not complete unless automated tests cover at least:

1. reviews disabled -> route unavailable (`404`) and zero writes,
2. missing review token -> `401` and zero writes,
3. wrong review token -> `401` and zero writes,
4. submission-write token -> `401` and zero writes,
5. wrong content type -> `415` and zero writes,
6. body over 16 KiB -> `413` and zero writes,
7. empty body -> `400` and zero writes,
8. malformed JSON -> `400` and zero writes,
9. unknown field, including any actor/audit field -> `400` and zero writes,
10. trailing JSON value -> `400` and zero writes,
11. `null`, array, or scalar JSON -> `400` and zero writes,
12. invalid/zero/overflow submission ID -> `400` and zero writes,
13. nonexistent submission -> `404`,
14. invalid decision -> `400` and zero writes,
15. empty or over-2,000-byte normalized reason -> `400` and zero writes,
16. approval revalidation failure -> `409`, submission remains pending, zero review events,
17. first approve -> `200`, one terminal event, no `RiskRule`,
18. first reject -> `200`, one terminal event, no `RiskRule`,
19. exact review retry -> `200`, same event ID, `replay=true`, no duplicate event,
20. different second decision -> `409`, no additional event,
21. different second reason -> `409`, no additional event,
22. different trusted actor label -> `409`, no additional event,
23. event-insert failure -> transaction rollback leaves submission pending,
24. existing PostgreSQL identical-review concurrency test remains one winner/event,
25. existing approve-vs-reject PostgreSQL concurrency test remains one terminal winner/event,
26. response/log capture does not expose either review or submission-write token,
27. success and failure paths never create/update/enable a `RiskRule`.

The repository-wide GitHub Actions gates remain mandatory, including backend tests, configured race tests, `go vet`, PostgreSQL 16, Redis 7, frontend install/typecheck/build.

## Implementation boundaries

The implementation PR after this design should be one bounded increment.

Expected production scope is approximately:

- a small review request/response DTO and handler in the rule module,
- route registration in `cmd/server` behind `RuleSubmissionReviewsEnabled`,
- reuse of `SubmissionReviewAuthorization`,
- focused transport/config integration tests,
- API documentation updated only once the route truly exists.

It must not include:

- review-specific Redis limiter,
- contributor identity,
- reviewer OAuth/RBAC,
- credential rotation framework,
- maintainer review-list API unless separately approved,
- frontend review UI,
- publication workflow,
- `RiskRule` mutation,
- review state-machine expansion,
- database schema redesign.

## Acceptance decision

After this document is merged, the transport contract is sufficiently frozen for a bounded implementation of controlled review HTTP mutation transport without inventing additional core security or business semantics during coding.
