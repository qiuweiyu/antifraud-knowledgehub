# Rule Submission Transport and Abuse-Control Design

This document freezes the minimum safety boundary that must exist before AntiFraud-KnowledgeHub exposes any network-facing rule-submission write path.

It is a design-only step for Issue #19. It does not add an HTTP endpoint, authentication, reviewer workflow, approval transitions, publication to `RiskRule`, frontend submission UI, bots, or AI review.

## Current baseline

The repository already has:

- rule draft validation through `ValidateDraft`,
- a non-executable `RuleSubmission` persistence model,
- `CreatePendingSubmission`, which validates before writing and forces `pending`,
- read-only pending-submission inspection,
- Redis connectivity in the application store,
- CORS and request logging middleware.

The repository does **not** currently have:

- an identity/authentication domain,
- a submission-specific authorization boundary,
- a submission rate limiter,
- a trusted-proxy policy for client IPs,
- a request-body size limiter,
- a public rule-submission route.

Therefore a public anonymous `POST /api/v1/rule-submissions` must not be added yet.

## Decision summary

1. Network submission is **disabled by default**.
2. The first HTTP transport slice is **controlled-access**, not anonymous public write.
3. The endpoint accepts JSON only and has a small bounded request body.
4. The endpoint must reuse `CreatePendingSubmission`; handlers may not bypass validation or write `RuleSubmission` directly.
5. A submission write credential must never be persisted or logged.
6. Rate limiting is mandatory before the route can be enabled.
7. Redis-backed write protection must fail closed when protection state cannot be evaluated.
8. Client-IP-based controls must not trust forwarded headers until trusted proxies are explicitly configured.
9. Exact replay/idempotency must be addressed before anonymous or broadly shared access is considered.
10. Request bodies, rule patterns, explanations, credentials, and personal data must not be written to application logs.
11. CORS is not an authorization mechanism.
12. Human review and publication remain separate later stages.

## Staged rollout

### Stage 0 — current state

No submission HTTP route exists.

Internal code can validate, persist, and inspect pending submissions in tests and future trusted application code.

### Stage 1 — controlled transport

The first network-facing implementation may add:

`POST /api/v1/rule-submissions`

but only when all of the following are true:

- `RULE_SUBMISSIONS_ENABLED=true` is explicitly configured,
- a dedicated write credential is configured,
- the request passes the submission rate limiter,
- the body passes the content-type and size checks,
- the JSON maps only to the existing draft contract,
- `CreatePendingSubmission` validates and persists the draft.

This stage exists to prove the transport and abuse-control boundary. It is not yet the final public community-authentication model.

### Stage 2 — community identity design

A later design may replace or complement the controlled write credential with a real contributor identity model, for example GitHub/OAuth-based identity.

Identity work must be designed separately. Do not invent placeholder user IDs in the current `RuleSubmission` schema.

### Stage 3 — review and publication

Only after submission transport is stable should the project add reviewer identity, approve/reject/change-requested semantics, audit events, revalidation, and publication to `RiskRule`.

## Feature flag

The future route must be disabled by default.

Recommended configuration:

`RULE_SUBMISSIONS_ENABLED=false`

If the flag is missing or false:

- do not register the write route, or
- return a stable disabled/not-found response according to the implementation decision.

Prefer not registering the route because it minimizes exposed surface.

Tests must prove the route is unavailable when the feature is disabled.

## Controlled write credential

The first transport slice should use a dedicated submission-write credential rather than reuse unrelated application secrets.

Recommended configuration name:

`RULE_SUBMISSION_WRITE_TOKEN`

Requirements:

- required whenever `RULE_SUBMISSIONS_ENABLED=true`,
- high-entropy secret supplied only through runtime configuration,
- never committed to the repository,
- never returned in API responses,
- never written to logs,
- compared using a timing-resistant comparison,
- sent in a dedicated authorization header rather than a query string.

A suitable first header contract is:

`Authorization: Bearer <token>`

This token is a temporary controlled-transport mechanism, not a long-term community identity model.

If the token is missing or invalid, reject before parsing or persisting a submission.

## Content type and body size

The future submission endpoint accepts:

`Content-Type: application/json`

Reject unsupported content types.

The first implementation should cap the entire HTTP request body at **32 KiB**.

Reasoning:

- a rule submission is structured metadata, not an attachment transport,
- `pattern`, `description`, `explanation`, and `recommendation` need reasonable space,
- a 32 KiB ceiling is comfortably above normal rule drafts while preventing accidental or hostile large-body ingestion.

The implementation must return a stable client error when the body exceeds the limit.

Do not accept:

- multipart uploads,
- files,
- images,
- screenshots,
- raw chat exports,
- arbitrary evidence blobs,
- nested unbounded metadata.

## Accepted request contract

The HTTP request must map only to the existing `DraftRequest` fields:

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

Unknown fields should be rejected once the transport is implemented, rather than silently retained as future accidental API surface.

The handler must not bind directly to `database.RuleSubmission`.

## Validation-before-write

The only supported write path remains:

`HTTP request -> transport checks -> DraftRequest -> CreatePendingSubmission -> ValidateDraft -> RuleSubmission`

The handler must not:

- call `db.Create(&RuleSubmission{})` directly,
- duplicate validation rules,
- choose the initial status,
- create or update a `RiskRule`.

Invalid drafts must produce zero submission writes.

Transport/auth/rate-limit failures must also produce zero submission writes.

## Rate limiting

A submission write route must not be enabled without a rate limiter.

The existing application already creates a Redis client, so the preferred first implementation is a small Redis-backed limiter dedicated to submission writes.

The limiter must be configurable rather than hard-coded as permanent product policy.

A conservative first default may be:

- 5 accepted submission attempts per 10 minutes per controlled credential,
- plus a global submission ceiling to reduce accidental runaway clients.

Exact default values can be adjusted during implementation, but tests must freeze whichever values are chosen.

### Failure behavior

For a write endpoint, rate-limit state is a safety dependency.

If Redis is required for the configured limiter and the limiter cannot evaluate the request because Redis is unavailable, the submission write path should **fail closed** with a service-unavailable response.

Do not silently disable rate limiting when its backend fails.

Read-only APIs do not need to inherit this write-specific failure policy.

## Client IP and proxy trust

Do not treat `X-Forwarded-For` or similar forwarded headers as trustworthy merely because they are present.

The project currently has no explicit trusted-proxy contract.

Therefore the first limiter should primarily key controlled transport by the write credential or another server-verified identity, not by untrusted forwarded IP headers.

If IP-based limits are added later:

1. define trusted proxies explicitly,
2. document deployment assumptions,
3. test spoofed forwarding headers,
4. keep IP only as one abuse signal, not contributor identity.

## Replay and idempotency

The existing persistence design intentionally allows more than one pending submission with the same rule `code`, because competing revisions may be legitimate.

Therefore `code` must not become an idempotency key.

Before access is broadened beyond controlled transport, add an explicit exact-replay mechanism based on a canonical normalized draft representation.

Recommended future direction:

- normalize the same fields used by the validator,
- generate a SHA-256 digest of the canonical draft,
- use the digest only for exact replay detection,
- do not treat semantically similar but non-identical drafts as duplicates,
- do not expose the digest as proof of contributor identity.

The exact database uniqueness/idempotency transaction semantics should be implemented in its own bounded task so concurrency behavior can be tested properly.

## Data safety

Submission transport must preserve the repository's existing security policy.

Do not accept or intentionally persist real:

- victim names,
- phone numbers used as personal evidence,
- bank-card numbers,
- government ID numbers,
- private chat identifiers,
- passwords,
- API keys,
- access tokens,
- private authentication cookies,
- unredacted screenshots.

Anti-fraud patterns may legitimately describe formats such as phone-number or bank-card regular expressions. Data-safety checks must distinguish a synthetic pattern from actual victim data and must not blindly reject every numeric-looking rule.

Human review remains required.

## Logging and observability

Submission request logging must be metadata-only.

Allowed examples:

- request ID,
- route,
- response status,
- latency,
- limiter outcome,
- validation success/failure count,
- created submission ID after a successful insert.

Do not log:

- Authorization headers,
- write credentials,
- raw request bodies,
- `pattern`,
- `description`,
- `explanation`,
- `recommendation`,
- personal data,
- secrets.

Validation error logs should prefer stable error codes/field names over rejected raw values.

## Response behavior

The future endpoint should use the project's existing response envelope.

Recommended status semantics:

- `201` — valid pending submission created,
- `400` — malformed JSON or invalid draft,
- `401` — missing/invalid controlled write credential,
- `413` — body too large,
- `415` — unsupported content type,
- `429` — rate limit exceeded,
- `503` — required abuse-control dependency unavailable.

Do not reveal whether a secret token was almost correct or provide internal limiter/database details.

## CORS

CORS is a browser-origin policy, not authentication or abuse prevention.

Do not treat an allowed origin as permission to write submissions.

The submission authorization and limiter checks must execute regardless of CORS configuration.

## Testing requirements for the future code slice

The first transport implementation must add automated tests for at least:

1. feature disabled -> write route unavailable,
2. enabled without configured write credential -> startup/configuration failure or safe route disablement,
3. missing token -> rejected, zero writes,
4. invalid token -> rejected, zero writes,
5. valid token + valid JSON -> one pending submission,
6. valid token + invalid draft -> zero writes,
7. unsupported content type -> rejected, zero writes,
8. oversized body -> rejected, zero writes,
9. rate limit exceeded -> `429`, zero writes,
10. Redis/limiter failure -> fail closed, zero writes,
11. no `RiskRule` is created or modified,
12. credentials and raw body do not appear in captured logs,
13. existing validation/persistence/inspection tests remain green.

The repository-wide gates remain mandatory:

- `go test ./...`
- `go vet ./...`
- frontend dependency install/typecheck/build through CI
- GitHub Actions must be green before merge.

## Next bounded implementation steps

Do not implement the whole transport in one large PR.

Recommended sequence:

1. **Submission transport configuration** — feature flag + dedicated write-token configuration + config tests; no route yet.
2. **Submission write authorization middleware/helper** — timing-safe token validation + tests; still no public route.
3. **Submission rate limiter** — Redis-backed limiter with fail-closed tests.
4. **Bounded JSON transport** — content-type/body limit + handler wired to `CreatePendingSubmission`.
5. **Exact replay/idempotency design and implementation** before broad community access.
6. **Community identity design** before replacing controlled access with true public contributor access.

Each step should be a separate small PR with its own tests and CI evidence.

## Explicit non-goals

This design does not add or authorize:

- anonymous public write access,
- user accounts,
- OAuth implementation,
- reviewer roles,
- approve/reject transitions,
- submission mutation/deletion,
- publication to `RiskRule`,
- attachments/evidence uploads,
- AI review or AI approval,
- browser-extension submission,
- a v0.2 release.

The safety boundary comes first. Community access can expand only after the transport proves it can reject unauthorized, oversized, repeated, or excessive writes without contaminating executable rules.