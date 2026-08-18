# Controlled Rule Submission Publication HTTP Transport Design

This document freezes the first bounded HTTP transport contract for controlled publication of an approved `RuleSubmission`.

It is a **design-only** step for Issue #55. It does not register a publication route, add a publication handler, add a Redis publication limiter, change database schema, add publisher identity/RBAC, add frontend publication UI, or change publication persistence/audit semantics.

For transport details, this document completes the deliberately deferred HTTP/security decisions in `rule-submission-publication-design.md`. It does not change that document's frozen approved-source integrity, current revalidation, atomic `RiskRule` + publication-event transaction, provenance, replay/conflict, hard-delete, or concurrency semantics.

Progress toward #1.

## Existing frozen dependencies

The implementation must reuse the current repository contracts rather than invent parallel behavior:

- `RULE_SUBMISSION_PUBLICATIONS_ENABLED` is default-off.
- `RULE_SUBMISSION_PUBLICATION_TOKEN` is independent from both `RULE_SUBMISSION_WRITE_TOKEN` and `RULE_SUBMISSION_REVIEW_TOKEN`.
- `RULE_SUBMISSION_PUBLICATION_ACTOR_LABEL` is server-owned, trimmed by the publication service, required when publication is enabled, and limited to 120 UTF-8 bytes.
- `SubmissionPublicationAuthorization` is the only publication Bearer-token authorization middleware for this slice.
- `PublishApprovedSubmission` is the only publication mutation service for this slice.
- Publication is allowed only from an approved `RuleSubmission` backed by the matching approved review event and digest integrity.
- Publication re-runs current `ValidateDraft` before first publication.
- `actor_kind` is fixed by the service to `controlled_publisher`.
- Publication creates one `RiskRule` and one `RuleSubmissionPublicationEvent` atomically.
- Exact same-actor replay returns the existing publication; a different publisher command or incompatible current state is a conflict.
- A publication event survives later `RiskRule` hard deletion, and retry after that hard deletion must not recreate the rule.
- The project response envelope remains `response.Envelope`.

## Route

Freeze the first controlled mutation route as:

```text
POST /api/v1/rule-submissions/:id/publications
```

Rationale:

- publication is modeled as a distinct audit resource, not a new submission status,
- the plural nested-resource shape matches the existing `/rule-submissions/:id/reviews` transport style,
- the endpoint represents creation/replay of the one publication event associated with the submission,
- do not use action-style alternatives such as `/publish` in this slice.

The route is registered **only** when `RULE_SUBMISSION_PUBLICATIONS_ENABLED=true` and configuration validation has succeeded.

When publications are disabled, the route is not registered. Requests therefore receive the router's ordinary `404` behavior. The disabled path must perform zero submission mutations, zero review-event mutations, zero `RiskRule` writes, and zero publication-event writes.

## Security and processing order

Freeze the processing order as:

```text
Publication feature gate / route registration
-> SubmissionPublicationAuthorization(publication token)
-> Content-Type validation
-> bounded request body
-> strict single-object JSON decoding
-> path submission ID parsing
-> server-owned publisher actor injection
-> PublishApprovedSubmission(...)
-> response mapping
```

Authorization intentionally runs before the handler parses the body or submission ID. An unauthorized caller must not be able to use publication transport to probe submission existence, approval state, publication state, validation state, or current `RiskRule` conflicts.

The handler must not add a second token parser/comparison implementation.

## No Publication Redis limiter in the first transport slice

Freeze **no Redis publication rate limiter** for this first controlled transport.

Rationale:

- the route is default-off,
- it is protected by an independent >=32-character controlled credential,
- only already-approved submissions can publish,
- first publication is transactionally bounded and exact retries are idempotent,
- database constraints remain authoritative for publication concurrency,
- introducing Redis here would create a new availability dependency and new security semantics that are not needed to expose the first maintainer-only transport safely.

This is not a statement that rate limiting can never be useful. A separate bounded design should be opened if deployment exposure, credential distribution, automation volume, or abuse evidence later justifies publication-specific throttling.

The existing public submission-write limiter must not be copied or reused implicitly.

## Authorization

The route must use:

```go
middleware.SubmissionPublicationAuthorization(cfg.RuleSubmissionPublicationToken)
```

Do not accept either the submission-write token or the review token for publication.

Existing shared strict Bearer behavior remains authoritative:

- missing or malformed Bearer header -> `401`,
- wrong token -> `401`,
- blank expected token -> fail closed with `401`,
- submission-write credential -> `401`,
- review credential -> `401`,
- correct independent publication token -> handler may execute,
- error response code remains `unauthorized`,
- credential material must never be reflected in responses or logs.

## Request media type

The endpoint accepts only a valid media type whose parsed base type is:

```text
application/json
```

Syntactically valid media-type parameters such as `charset=utf-8` are allowed, matching the repository's existing strict JSON transports.

Missing, malformed, or non-JSON content type returns:

```text
415 Unsupported Media Type
code: unsupported_media_type
message: rule submission publications require application/json
```

This failure occurs before body decoding and must cause zero database writes.

## Request body boundary

Freeze the maximum raw HTTP request body at **4 KiB**.

The publication client has no business fields to submit; the only valid payload is an empty JSON object. Four KiB is therefore intentionally generous for whitespace/transport tolerance while still placing a small hard bound before JSON decoding.

The implementation should use `http.MaxBytesReader` before JSON decoding.

A body exceeding the limit returns:

```text
413 Request Entity Too Large
code: publication_body_too_large
message: rule submission publication body exceeds 4 KiB
```

No partial body processing may produce a publication mutation.

## Client JSON contract

The only accepted client object is:

```json
{}
```

There are **no client-owned publication command fields** in this first transport contract.

The client must never control or supply:

- `actor_kind`,
- `actor_label`,
- `submission_id`,
- submission/review status,
- review event ID,
- review decision/reason,
- `draft_digest`,
- publication event ID,
- publication timestamps,
- `risk_rule_id`,
- `risk_rule_code`,
- rule code/name/category/type/pattern/weight/severity/enabled/explanation/recommendation,
- `source_submission_id`,
- any force/override/recreate flag,
- any `RiskRule` lifecycle field.

In particular, do not introduce client fields such as `publish`, `confirm`, `force`, `actor`, or a rule snapshot merely because the HTTP method is `POST`. Human approval and controlled publisher authorization already provide the workflow authority; publication data comes from the approved stored snapshot.

## Strict JSON semantics

Use `encoding/json.Decoder` with unknown-field rejection and an empty request DTO/object shape.

Freeze these rules:

- body must contain exactly one JSON object,
- that object must contain no fields,
- empty body is invalid,
- malformed JSON is invalid,
- any JSON field is invalid,
- a second/trailing JSON value is invalid,
- trailing JSON whitespace is allowed,
- JSON `null` is invalid because it is not the required publication object,
- arrays and scalar JSON values are invalid.

All decode/shape failures return:

```text
400 Bad Request
code: invalid_publication_json
message: request body must be a single empty JSON object
```

Decode failures must occur before `PublishApprovedSubmission` is called.

## Submission ID parsing

The Gin path parameter `:id` must be parsed as a strict positive base-10 unsigned integer, using the same semantics already frozen for review transport.

Accepted forms include:

```text
1
42
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
- overflow for the implementation's `uint` target.

Invalid ID returns:

```text
400 Bad Request
code: invalid_submission_id
message: rule submission id must be a positive decimal integer
```

This failure must occur before the publication service is called.

The implementation may reuse/refactor the existing strict review ID parser inside the rule package if doing so preserves the already-tested review semantics. It must not broaden accepted ID syntax as an unrelated refactor.

## Server-owned publisher attribution

The request body never supplies actor information.

The handler constructs the service command from:

```text
actor_label <- cfg.RuleSubmissionPublicationActorLabel
actor_kind  <- fixed inside PublishApprovedSubmission as controlled_publisher
```

`RULE_SUBMISSION_PUBLICATION_ACTOR_LABEL` remains an operational attribution label, not proof of a specific person's authenticated identity.

Do not describe this transport as OAuth, RBAC, publisher-user authentication, non-repudiation, or cryptographic identity.

## Service error mapping

The handler must map the existing publication service/database result classes deterministically and must not expose raw service/database error strings.

### Missing submission

`gorm.ErrRecordNotFound` from loading the requested submission maps to:

```text
404 Not Found
code: submission_not_found
message: rule submission not found
```

### Submission not approved/publishable

`ErrSubmissionNotPublishable` maps to:

```text
409 Conflict
code: submission_not_publishable
message: rule submission is not publishable
```

Pending and rejected submissions therefore do not receive separate external status-specific error codes.

### Publication-time current validation failure

`ErrSubmissionPublicationValidation` maps to:

```text
409 Conflict
code: submission_publication_validation_failed
message: rule submission is no longer valid for publication
```

This includes current validation becoming invalid after approval, such as category removal or an already-existing conflicting rule code.

The first transport slice does not extend the common API error envelope with validation-detail payloads. Do not return stored pattern/free text, draft snapshot, digest, raw validation internals, or raw database errors.

### Publication conflict

`ErrSubmissionPublicationConflict` maps to:

```text
409 Conflict
code: submission_publication_conflict
message: rule submission publication conflicts with current state
```

This class intentionally covers bounded incompatible states including:

- an existing publication made by a different trusted publisher label,
- retry after the publication's current `RiskRule` was hard-deleted (must not recreate),
- a same-code concurrent winner from another source,
- other service-defined publication conflicts.

Do not map the hard-delete retry case to `404`; a historical publication exists and recreation is intentionally forbidden.

### Publication integrity failure

`ErrSubmissionPublicationIntegrity` maps to:

```text
500 Internal Server Error
code: submission_publication_integrity_error
message: rule submission publication integrity check failed
```

Integrity failures include missing/mismatched approved review provenance, digest disagreement, or inconsistent stored publication provenance. They must not be converted to `409` merely to make the API appear recoverable.

### Invalid server-owned publication command

`ErrInvalidSubmissionPublication` is **not** a client `400` in this HTTP contract because the only command field (`ActorLabel`) is server-owned and startup configuration validation should already have rejected an invalid value.

If this error nevertheless reaches the handler, map it to the same generic internal failure as other unexpected server/service failures:

```text
500 Internal Server Error
code: submission_publication_failed
message: rule submission publication could not be completed
```

Do not expose actor/configuration internals to the client.

### Other service/database failures

Any other unexpected error maps to:

```text
500 Internal Server Error
code: submission_publication_failed
message: rule submission publication could not be completed
```

No internal SQL, DSN, table name, credential, stored free text, digest, or raw GORM error is returned to the client.

## Success response and exact replay

A **newly committed publication** returns:

```text
201 Created
```

An **exact same-actor replay** returns:

```text
200 OK
```

This distinction lets controlled automation tell whether it created the publication in the current request without inventing a second mutation or lookup endpoint.

The handler must return an explicit transport DTO rather than serialize GORM persistence structs directly.

Freeze the successful `data` shape as:

```json
{
  "submission_id": 123,
  "status": "approved",
  "review_event_id": 456,
  "publication_event_id": 789,
  "risk_rule_id": 321,
  "risk_rule_code": "fake_support_remote_control",
  "actor_kind": "controlled_publisher",
  "actor_label": "publisher-console",
  "created_at": "2026-08-18T13:30:00Z",
  "replay": false
}
```

Field sources are intentionally audit-oriented:

- `submission_id` comes from the stored publication event/submission relationship,
- `status` is the stored submission status and remains `approved`; publication does not add a `published` submission status,
- `review_event_id` comes from the publication event provenance,
- `publication_event_id` identifies the one publication audit event,
- `risk_rule_id` comes from the publication event's initial rule identity,
- `risk_rule_code` comes from the publication event's frozen initial code, **not** from mutable current `RiskRule` fields,
- `actor_kind` and `actor_label` come from the stored publication event,
- `created_at` is the publication-event timestamp encoded with Go's normal JSON time representation,
- `replay` is `false` for the transaction winner and `true` for an exact replay.

Do **not** include in this first mutation response:

- rule pattern or other rule snapshot fields,
- current mutable `RiskRule` state,
- `source_submission_id`,
- draft/review digest,
- raw review reason,
- validation internals,
- submission/review/publication credentials,
- database/internal error details.

If the current `RiskRule` has been hard-deleted, the service returns conflict and no success response is produced.

## Approval remains distinct from publication

A successful publication must preserve:

```text
RuleSubmission.status = approved
approved review event unchanged
exactly one publication event for the source submission
exactly one publication-created RiskRule for the winning first publication
```

The handler must call `PublishApprovedSubmission`; it must not mutate review state, manufacture a new review event, bind rule snapshot fields from the client, call the ordinary direct-rule create handler, or directly write `RiskRule`/publication-event rows.

## Zero-write failure boundary

The future implementation tests must prove that these transport failures execute no publication mutation and create no `RiskRule`/publication event:

- feature disabled,
- missing publication token,
- malformed publication token,
- wrong publication token,
- submission-write token used as publication token,
- review token used as publication token,
- unsupported/malformed content type,
- oversized body,
- empty body,
- malformed JSON,
- any unknown JSON field (including actor, rule, digest, review, force/override fields),
- trailing JSON value,
- `null`, array, or scalar JSON,
- invalid submission ID,
- nonexistent submission,
- pending submission,
- rejected submission,
- publication-time current validation failure,
- approved-source/publication integrity failure,
- unexpected service/database failure before commit.

For all failed publication requests:

- `RuleSubmission.status` must remain unchanged,
- the existing approved review event/reason must remain unchanged,
- no new review event may be created,
- no partially created `RiskRule` may remain committed,
- no partial publication event may remain committed.

## Logging and data safety

Request logging remains metadata-only.

Allowed publication operational metadata may include:

- HTTP route/status/latency,
- parsed submission ID after authorization,
- publication created/replay/conflict outcome,
- resulting publication event ID and risk rule ID/code after success,
- stable validation field/error codes if later logged by service code,
- trusted actor label only if operators continue to classify it as non-secret.

Do not log by default:

- Authorization header,
- publication/review/write token,
- raw request body,
- stored rule pattern or free text,
- full submission snapshot,
- canonical draft JSON,
- raw review reason,
- draft digest,
- database DSN or SQL containing user material.

The endpoint is not an evidence-upload channel.

## Required implementation test matrix

The future code PR is not complete unless automated tests cover at least:

1. publications disabled -> route unavailable (`404`) and zero publication writes,
2. missing publication token -> `401` and zero writes,
3. malformed publication token -> `401` and zero writes,
4. wrong publication token -> `401` and zero writes,
5. submission-write token -> `401` and zero writes,
6. review token -> `401` and zero writes,
7. wrong/malformed content type -> `415` and zero writes,
8. body over 4 KiB -> `413` and zero writes,
9. empty body -> `400` and zero writes,
10. malformed JSON -> `400` and zero writes,
11. any unknown field, including actor/rule/digest/review/force fields -> `400` and zero writes,
12. trailing JSON value -> `400` and zero writes,
13. `null`, array, or scalar JSON -> `400` and zero writes,
14. invalid/zero/overflow submission ID -> `400` and zero writes,
15. nonexistent submission -> `404` and zero writes,
16. pending submission -> `409` and zero writes,
17. rejected submission -> `409` and zero writes,
18. publication-time current validation failure -> `409`, approved submission/review unchanged, zero publication writes,
19. approved-source digest/review integrity failure -> `500` and zero publication writes,
20. first valid publication -> `201`, one `RiskRule`, one publication event, approved submission/review unchanged,
21. first publication faithfully preserves the approved `Enabled=false` snapshot in the committed `RiskRule`,
22. success response uses publication-event provenance and does not expose snapshot/digest/review reason/token material,
23. exact same-actor retry -> `200`, same event/rule IDs, `replay=true`, no duplicates,
24. different trusted actor label after publication -> `409`, no duplicate rule/event,
25. retry after published `RiskRule` hard deletion -> `409`, publication event remains, rule is not recreated,
26. publication event insert failure -> `500` and transaction rollback leaves no committed `RiskRule`,
27. successful or failed publication never changes the terminal approved review event/decision/reason,
28. existing real PostgreSQL same-submission/same-actor concurrency test remains one rule/event winner with safe replay,
29. existing real PostgreSQL different-actor concurrency test remains one winner and one conflict,
30. existing real PostgreSQL same-code competing submissions test remains exactly one winning rule/publication event,
31. response/log capture does not expose publication, review, or submission-write credentials,
32. repository-wide GitHub Actions backend Test/Race/Vet with PostgreSQL 16 and Redis 7 plus frontend Install/Typecheck/Build remain green.

## Implementation boundaries

The implementation PR after this design should be one bounded increment.

Expected production scope is approximately:

- a small publication request/response DTO and handler in the rule module,
- route registration in `cmd/server` behind `RuleSubmissionPublicationsEnabled`,
- reuse of `SubmissionPublicationAuthorization`,
- reuse/refactor of strict JSON/positive-ID helper logic only when semantics remain unchanged,
- focused transport tests including zero-write security cases,
- API documentation updated only once the route truly exists.

It must not include:

- publication-specific Redis limiter,
- contributor/publisher identity,
- OAuth/RBAC,
- credential rotation framework,
- publication list/read API unless separately approved,
- frontend publication UI,
- review/submission state-machine expansion,
- `RiskRule` lifecycle redesign,
- database schema redesign,
- publication persistence/audit redesign.

## Acceptance decision

After this document is merged, the first controlled publication HTTP mutation contract is sufficiently frozen for a bounded implementation without inventing additional core transport, security, audit, or business semantics during coding.
