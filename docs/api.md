# API

Base path: `/api/v1`

## Health

`GET /health`

```json
{"status":"ok","service":"antifraud-knowledgehub"}
```

## Categories

- `GET /categories`
- `GET /categories/{id}`

Persisted category content is read-only through the default public API. There is no anonymous HTTP endpoint for creating, updating, or deleting categories. Maintainer changes may be made through reviewed repository seed-data changes or a future separately designed and authorized mutation capability.

## Rules

- `GET /rules?q=&category_code=&severity=`
- `POST /rules/validate`
- `GET /rules/{id}`

Persisted `RiskRule` content is read-only through the default public API. Direct anonymous create/update/toggle/delete routes are intentionally not registered. The current HTTP mutation path for rules is the independently authorized, default-off Rule Submission -> Review -> Publication workflow documented below.

### Validate Rule Draft

`POST /api/v1/rules/validate`

Validates a rule draft before it is saved. This endpoint does not create a
`RiskRule` record and has no database write side effects.

Request:

```json
{
  "code": "community_safe_channel",
  "name": "Official channel check",
  "category_code": "fake_customer_service",
  "rule_type": "keyword",
  "pattern": "official channel",
  "weight": 20,
  "severity": "medium"
}
```

Response:

```json
{
  "success": true,
  "data": {
    "valid": true,
    "errors": [],
    "warnings": []
  }
}
```

## Controlled Rule Submissions

`POST /api/v1/rule-submissions`

This endpoint is a controlled maintainer transport, not anonymous public community access. It is registered only when `RULE_SUBMISSIONS_ENABLED=true` and the submission transport configuration is valid.

Requirements:

- `Authorization: Bearer <RULE_SUBMISSION_WRITE_TOKEN>`
- `Content-Type: application/json`
- request body no larger than 32 KiB
- JSON must contain only fields supported by the existing rule `DraftRequest`
- Redis-backed per-credential and global rate limits must be available

Persistence delegates to the replay-safe pending-submission service. A new valid canonical draft creates one non-executable `RuleSubmission` with server-assigned status `pending`; an exact retry returns the already existing pending submission instead of creating another row. Neither create nor replay creates or modifies a `RiskRule`.

Exact replay is based on a server-computed SHA-256 fingerprint of the normalized persisted draft snapshot. The digest is internal: clients do not send it and it is not exposed in the response. Same `code` with different persisted content remains a distinct proposal. Authorization and Redis rate limiting run before replay detection, so retries still consume normal abuse-control budget.

Example request:

```json
{
  "code": "community_safe_account_request",
  "name": "Safe account transfer request",
  "category_code": "fake_customer_service",
  "rule_type": "keyword",
  "pattern": "安全账户",
  "weight": 40,
  "severity": "high",
  "explanation": "Synthetic anti-fraud example.",
  "recommendation": "Verify the request independently before transferring funds."
}
```

Important response statuses:

- `201` — one new pending submission created
- `200` — exact replay; the existing pending submission is returned with the same ID
- `400` — malformed JSON, unknown fields, trailing JSON, or invalid rule draft for a new digest
- `401` — missing or invalid controlled write credential
- `413` — request body exceeds 32 KiB
- `415` — unsupported content type
- `429` — submission rate limit exceeded
- `503` — Redis/rate-limiter protection cannot be evaluated

The route is intentionally unavailable when the feature flag is disabled. Credentials, raw submission bodies, canonical JSON, and draft digests must not be logged.

## Controlled Rule Submission Reviews

`POST /api/v1/rule-submissions/{id}/reviews`

This is the first controlled maintainer review mutation transport. It is registered only when `RULE_SUBMISSION_REVIEWS_ENABLED=true` and valid independent review credentials and a trusted actor label are configured. It is not public reviewer identity, OAuth, or RBAC.

Requirements:

- `Authorization: Bearer <RULE_SUBMISSION_REVIEW_TOKEN>`
- the review token must be independent from `RULE_SUBMISSION_WRITE_TOKEN`
- `Content-Type: application/json`
- request body no larger than 16 KiB
- `id` must be a positive decimal integer
- JSON must be a single object containing only `decision` and `reason`
- `decision` is `approved` or `rejected`
- normalized `reason` is required and limited to 2,000 UTF-8 bytes
- actor attribution comes only from `RULE_SUBMISSION_REVIEW_ACTOR_LABEL`

Example request:

```json
{
  "decision": "approved",
  "reason": "Matches the documented fraud pattern and has an acceptable false-positive profile."
}
```

A successful first review and an exact retry both return `200`. The response contains the terminal submission status, the single review-event ID, server-owned actor attribution, event creation time, and a `replay` flag. It does not return the review reason, draft snapshot, draft digest, review token, or publication state.

Example success data:

```json
{
  "success": true,
  "data": {
    "submission_id": 123,
    "status": "approved",
    "review_event_id": 456,
    "decision": "approved",
    "actor_kind": "controlled_maintainer",
    "actor_label": "maintainer-console",
    "created_at": "2026-08-18T11:30:00Z",
    "replay": false
  }
}
```

Important response statuses:

- `200` — terminal review committed, or exact review replay returned
- `400` — invalid review JSON, invalid positive-decimal submission ID, decision, or reason
- `401` — missing or invalid independent review credential
- `404` — submission does not exist, or the review feature is disabled and the route is not registered
- `409` — approval no longer passes current validation, or a different terminal review already exists
- `413` — request body exceeds 16 KiB
- `415` — unsupported content type
- `500` — review integrity or unexpected persistence failure

Approval means the proposal has been accepted for a future publication step. It does **not** create, update, enable, or otherwise publish a `RiskRule`. Exact review replay returns the existing review event; a different second decision, reason, or trusted actor attribution is a conflict. There is no review-specific Redis limiter in this transport slice.

## Controlled Rule Submission Publications

`POST /api/v1/rule-submissions/{id}/publications`

This endpoint completes the controlled submit -> review -> publish workflow. It is registered only when `RULE_SUBMISSION_PUBLICATIONS_ENABLED=true` and valid independent publication credentials and a trusted publisher actor label are configured. Publication remains a maintainer-controlled operation; this credential is not OAuth/RBAC or proof of a specific person's identity.

Requirements:

- `Authorization: Bearer <RULE_SUBMISSION_PUBLICATION_TOKEN>`
- the publication token must be independent from both write and review tokens
- `Content-Type: application/json`
- request body no larger than 4 KiB
- `id` must be a positive decimal integer
- the only valid client body is one strict empty JSON object: `{}`
- any client-supplied actor, rule, review, digest, provenance, force/override, or recreation field is rejected
- actor attribution comes only from `RULE_SUBMISSION_PUBLICATION_ACTOR_LABEL`
- no publication-specific Redis limiter is used in this first controlled transport

Example request:

```json
{}
```

The server publishes only the already-approved stored submission snapshot. `PublishApprovedSubmission` revalidates that snapshot against current rule constraints, verifies approved review/digest provenance, and atomically creates one `RiskRule` plus one publication event. Publication does not change the submission from `approved` to a new status and does not rewrite the approved review event.

Example first-publication success data:

```json
{
  "success": true,
  "data": {
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
}
```

Important response statuses:

- `201` — one new publication committed
- `200` — exact same-actor replay; the existing publication event/rule identity is returned with `replay=true`
- `400` — invalid publication JSON or invalid positive-decimal submission ID
- `401` — missing or invalid independent publication credential
- `404` — submission does not exist, or the publication feature is disabled and the route is not registered
- `409` — submission is not publishable, current validation failed, publication conflicts with current state, or a previously published rule was later removed out-of-band from persistence
- `413` — request body exceeds 4 KiB
- `415` — unsupported content type
- `500` — publication integrity or unexpected persistence failure

The success response is audit/provenance-oriented. It does not return the rule snapshot, mutable current `RiskRule` fields, review reason, draft digest, source provenance internals, credentials, SQL, or raw persistence errors. If a publication-created `RiskRule` is later removed out-of-band from persistence, retry returns conflict and never recreates that rule.

## Cases

- `GET /cases?q=&category_code=`
- `GET /cases/{id}`

Persisted scam-case content is read-only through the default public API. There is no anonymous HTTP endpoint for creating, updating, or deleting cases. Maintainer changes may be made through reviewed repository seed-data changes or a future separately designed and authorized mutation capability.

## Text Analysis

`POST /analysis/text`

```json
{"text":"客服说账户异常，需要转账到安全账户"}
```

The response includes `risk_score`, `risk_level`, `matched_rules`, `summary` and `recommendations`. This historical route remains deterministic-only and writes its normal `AnalysisRecord`; configuring an LLM provider does not make this route send text to a third party.

## Preview Analysis

`POST /analysis/preview`

Uses the same deterministic rule engine result but creates zero `AnalysisRecord` rows. It remains deterministic-only and never invokes the configured LLM provider.

## Explicit Assisted Analysis

`POST /analysis/assisted`

This route is registered only when `LLM_ASSISTED_ANALYSIS_HTTP_ENABLED=true`. It is the first explicit opt-in, potentially cost-bearing route that may send submitted text and deterministic rule context to the server-configured third-party LLM provider.

Requirements:

- `LLM_ASSISTANCE_ENABLED=true` with a valid server-selected provider/model/credential;
- `Authorization: Bearer <LLM_ASSISTED_ANALYSIS_TOKEN>` using a credential independent from rule submission/review/publication credentials;
- Redis-backed per-credential and global assisted-analysis limits must be available;
- `Content-Type: application/json`;
- body no larger than 16 KiB;
- a single strict JSON object containing only `text`;
- source text must be non-blank and no larger than 12 KiB UTF-8;
- clients cannot select provider, model, API key, base URL, tools, or provider options.

Example request:

```json
{
  "text": "客服称账户异常，要求立即转账到所谓安全账户"
}
```

Example success data:

```json
{
  "success": true,
  "data": {
    "rule_result": {
      "risk_score": 80,
      "risk_level": "high",
      "matched_rules": [],
      "summary": "...",
      "recommendations": []
    },
    "llm_assistance": {
      "status": "available",
      "provider": "gemini",
      "model": "server-configured-model",
      "assistance": {
        "summary": "...",
        "observations": [],
        "limitations": []
      }
    }
  }
}
```

`rule_result` is the authoritative deterministic anti-fraud result. `llm_assistance` is supplemental only. If the provider times out, refuses, returns malformed output, or is otherwise unavailable after deterministic analysis succeeds, the HTTP response remains `200`, the rule result is unchanged, and `llm_assistance.status` becomes `unavailable`; raw provider errors are never returned.

The first assisted route creates zero `AnalysisRecord` rows and does not persist the prompt, provider response, or hidden reasoning. It also performs no automatic provider retry and makes no exactly-once execution or billing claim across client/network retries.

Important response statuses before provider invocation:

- `400` — invalid JSON/envelope/text
- `401` — missing or invalid assisted-analysis Bearer credential
- `404` — feature disabled and route therefore not registered
- `413` — request body exceeds 16 KiB
- `415` — unsupported content type
- `429` — assisted-analysis rate limit exceeded
- `503` — Redis limiter unavailable, or deterministic enabled-rule loading failed

### Privacy boundary

Calling `/analysis/assisted` is an explicit third-party data-transfer action. The server sends the submitted text plus deterministic rule context to the configured provider selected by the operator (`openai`, `gemini`, or `deepseek` in the current build). Provider retention and data-control behavior depends on that provider and the account configuration. Provider API credentials stay server-side and are never accepted from or returned to the caller.

In contrast, `/analysis/text` and `/analysis/preview` do not invoke an LLM provider.

## Controlled Browser Session

The browser bridge is registered only when `BROWSER_ASSISTED_ENABLED=true` with a valid exact origin, digest-only access-grant registry and Redis backend. It is intended for controlled beta access, not anonymous public cost-bearing LLM use.

### Exchange Browser Access Grant

`POST /browser/session/exchange`

Request:

```json
{
  "access_grant": "<operator-provisioned raw grant>"
}
```

The raw grant is high-entropy material provisioned outside the application. Server configuration stores only its SHA-256 digest. A successful exchange creates an opaque one-hour Redis session and sets an HttpOnly session cookie; the raw session token is never returned in JSON.

Required protections include:

- exact configured `Origin` validation before grant verification;
- strict `application/json` body handling and a bounded request body;
- trusted-peer-aware source extraction;
- independent Redis source/global pre-auth rate admission;
- generic authentication failure responses that do not reveal principal details;
- `Cache-Control: no-store`.

The response contains bounded principal/session metadata and a derived CSRF token for current in-memory browser use. The production cookie is `__Host-afkh_browser_session`, `Secure`, `HttpOnly`, `SameSite=Strict`, `Path=/`, with no `Domain` attribute.

### Read Current Browser Session

`GET /browser/session`

Requires a valid browser session cookie. Validation re-resolves the configured principal and `principal_generation`, so disabling/removing the principal or incrementing generation revokes existing sessions.

The response contains non-secret principal metadata, absolute expiry and a derived CSRF token. It never exposes the raw session token or Browser Access Grant.

### Logout Browser Session

`POST /browser/session/logout`

Requires:

- exact configured `Origin`;
- valid current session cookie;
- `X-AFKH-CSRF: <derived csrf token>`.

Successful logout deletes the Redis session state and clears the cookie. Session/Redis validation fails closed.

## Browser Assisted Profile Metadata

`GET /browser/analysis/assisted/profiles`

Registered only when both the browser session bridge and browser assisted-analysis capability are enabled. Requires a valid browser session.

The response contains only bounded public profile metadata such as:

```json
{
  "id": "default",
  "display_name": "AI assisted analysis",
  "provider_display_name": "OpenAI",
  "model_display_name": "Server-approved model",
  "availability": "available",
  "disclosure": "Submitted text may be sent to the configured third-party AI provider."
}
```

It never exposes provider API keys, internal endpoints/base URLs, authorization material, hidden prompts or privileged provider configuration.

## Browser Assisted Analysis

`POST /browser/analysis/assisted`

This is the Vue-facing controlled assisted-analysis endpoint. It is registered only when:

- `BROWSER_ASSISTED_ENABLED=true`;
- `BROWSER_ASSISTED_ANALYSIS_ENABLED=true`;
- `LLM_ASSISTANCE_ENABLED=true` with valid server-owned provider/profile configuration.

Request:

```json
{
  "text": "客服称账户异常，要求立即转账到所谓安全账户",
  "profile_id": "default"
}
```

The browser may send only `text` and a server-approved `profile_id`. It cannot choose provider, model, API key, base URL, endpoint, tools, retry policy, timeout or output budget.

The enforced execution order is:

1. exact configured Origin;
2. Redis session validation including current principal/generation;
3. `X-AFKH-CSRF` validation;
4. atomic Redis per-principal/global cost admission;
5. strict bounded JSON decode;
6. server-owned profile resolution;
7. strict enabled-rule database load;
8. deterministic risk-engine analysis;
9. exactly one selected server-owned assistance service call;
10. deterministic result plus supplemental assistance response.

Important negative-path properties:

- unauthenticated, Origin-rejected or CSRF-rejected requests stop before analysis/provider work;
- rate denial stops before provider work;
- invalid body/profile and database rule-load failures produce zero provider calls;
- Redis nil/error/timeout fails closed;
- provider failure after deterministic success does not retry and returns HTTP `200` with the deterministic result unchanged and supplemental status `unavailable`;
- the route creates no new `AnalysisRecord` and does not persist raw prompts/provider responses/hidden reasoning;
- responses use `Cache-Control: no-store`.

The Vue client additionally fails closed if its configured API base resolves cross-origin. Browser Access Grants are transient and are not stored in localStorage, sessionStorage or IndexedDB; the CSRF token is kept only in current in-memory UI state. Provider credentials and the maintainer `LLM_ASSISTED_ANALYSIS_TOKEN` must never be embedded in browser code.

## Analysis Stats

`GET /analysis/stats`

Returns aggregate counts for categories, rules, enabled rules, anonymous cases, analysis records, risk-level buckets and category coverage. The dashboard overview page uses this endpoint instead of loading every rule and case just to calculate counts.

## Response Envelope

Successful responses use:

```json
{"success":true,"data":{}}
```

Errors use:

```json
{"success":false,"error":{"code":"invalid_request","message":"human readable message"}}
```
