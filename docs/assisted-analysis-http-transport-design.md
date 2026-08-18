# Explicit Opt-in Assisted Analysis HTTP Transport

Status: **Frozen for implementation**  
Parent issue: #5  
Design issue: #84

## 1. Goal

Add one explicitly opt-in HTTP route that combines the existing deterministic anti-fraud rule result with supplemental assistance from the server-configured LLM provider.

The route must not change the behavior of the existing deterministic endpoints and must not create unauthenticated, unbounded, or silent third-party data transfer.

## 2. Endpoint

Freeze the first route as:

```text
POST /api/v1/analysis/assisted
```

This is the only first-slice HTTP route allowed to invoke an external LLM provider.

Existing routes remain deterministic-only:

```text
POST /api/v1/analysis/text
POST /api/v1/analysis/preview
```

Enabling LLM configuration must never change those two routes.

## 3. Provider selection

Provider and model are server/operator configuration:

```text
LLM_ASSISTANCE_PROVIDER
LLM_ASSISTANCE_MODEL
```

The client request cannot contain:

- provider;
- model;
- API key;
- base URL;
- endpoint;
- tools;
- provider-specific options.

The first runtime supports one selected provider/model per process.

The response may disclose provider/model as server-owned transparency metadata because neither value is a credential.

## 4. Independent HTTP feature gate

Add:

```text
LLM_ASSISTED_ANALYSIS_HTTP_ENABLED=false
```

When false:

- `/api/v1/analysis/assisted` is not registered;
- request receives normal router 404 behavior;
- zero authorization middleware work;
- zero Redis work;
- zero DB work;
- zero provider work.

When true:

- `LLM_ASSISTANCE_ENABLED` must also be true;
- configured provider/model/credential must already pass the existing Registry/provider validation;
- route-specific auth/rate settings must validate before server startup.

## 5. Independent transport credential

Add:

```text
LLM_ASSISTED_ANALYSIS_TOKEN
```

Requirements when HTTP transport is enabled:

- trim-nonempty;
- minimum 32 characters;
- used only as the route Bearer credential;
- never reused as an OpenAI/Gemini/DeepSeek API key;
- must differ from configured `RULE_SUBMISSION_WRITE_TOKEN`;
- must differ from configured `RULE_SUBMISSION_REVIEW_TOKEN`;
- must differ from configured `RULE_SUBMISSION_PUBLICATION_TOKEN`.

Authorization reuses the existing strict Bearer parser and constant-time digest comparison primitive.

Unauthorized response remains generic and never identifies whether provider configuration exists.

## 6. Independent Redis rate controls

Add:

```text
LLM_ASSISTED_ANALYSIS_CREDENTIAL_LIMIT=10
LLM_ASSISTED_ANALYSIS_GLOBAL_LIMIT=50
LLM_ASSISTED_ANALYSIS_RATE_WINDOW=1m
```

Validation:

- credential limit > 0;
- global limit > 0;
- global limit >= credential limit;
- window >= 1ms.

Use an independent Redis namespace:

```text
afkh:llm-assisted-analysis:rate:credential:<sha256-token>
afkh:llm-assisted-analysis:rate:global
```

The raw route token is never placed in a Redis key.

The per-credential and global counters must be incremented atomically in one Redis script/operation with the same window.

Redis timeout budget should remain bounded (500ms is acceptable for the first slice).

Failure semantics:

- Redis client nil -> 503;
- Redis error/timeout -> 503;
- limit exceeded -> 429;
- all three paths abort before body parsing, DB query, or provider invocation.

The new limiter must not use `afkh:rule-submission:*` counters or credentials.

## 7. Exact request processing order

Freeze this order:

```text
conditional route registration
        ↓
strict Bearer authorization
        ↓
fail-closed Redis rate limit
        ↓
Content-Type validation
        ↓
16 KiB HTTP body limit
        ↓
strict JSON decoding
        ↓
12 KiB source-text validation
        ↓
strict DB rule load
        ↓
deterministic risk engine
        ↓
llmassist.Service
        ↓
combined response
```

No later stage may execute when an earlier stage fails.

## 8. Request content type and body

Require `Content-Type: application/json`.

The media type may include valid parameters such as charset, but the parsed media type must be exactly `application/json`.

Maximum HTTP body size:

```text
16 KiB
```

This is intentionally slightly larger than the source-text bound to allow JSON escaping/envelope overhead.

## 9. Strict JSON contract

Only this object is legal:

```json
{
  "text": "suspicious text"
}
```

Reject:

- empty body;
- `null`;
- arrays;
- scalars;
- unknown fields;
- duplicate/trailing second JSON values;
- missing `text`;
- blank/whitespace-only text.

Do not accept provider/model overrides in the body.

## 10. Source text bound

Reject source text over:

```text
12 KiB UTF-8 bytes
```

before DB/provider work.

Do not silently truncate.

The provider adapters retain their own 12 KiB defensive checks as a second boundary.

## 11. Deterministic rule loading

The assisted route must not use a DB helper that silently ignores a failed rule query.

Before any provider call:

1. query enabled `RiskRule` rows;
2. check the GORM query error explicitly;
3. map rows to `riskengine.Rule`;
4. compute deterministic `riskengine.Result`.

If the DB query fails:

- return generic analysis/service unavailable error;
- do not call the LLM provider;
- do not persist an analysis row.

The first implementation may introduce a strict assisted-only helper rather than changing the historical `/analysis/text` and `/analysis/preview` error semantics.

## 12. LLM service invocation

After deterministic analysis succeeds:

```go
outcome := llmService.Assist(ctx, llmassist.Input{
    Text: text,
    RuleResult: ruleResult,
})
```

The existing Service remains responsible for:

- deep-copy isolation;
- timeout/cancellation;
- provider-error normalization;
- supplemental-output bounds.

The server performs no provider retry.

## 13. Response DTO

Freeze a transport DTO that makes authority explicit:

```json
{
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
    "model": "configured-model",
    "assistance": {
      "summary": "...",
      "observations": [],
      "limitations": []
    }
  }
}
```

`rule_result` is authoritative.

`llm_assistance` is supplemental.

The transport must not flatten the two objects into one namespace.

## 14. Provider failure response

If deterministic analysis succeeds but the external provider fails, times out, refuses, returns malformed output, or is otherwise unavailable:

- HTTP status remains 200;
- `rule_result` remains unchanged;
- `llm_assistance.status` is `unavailable`;
- provider/model may remain visible as server-selected metadata;
- `assistance` is the zero/empty supplemental object;
- no raw provider error is returned.

This prevents external-provider availability from becoming the availability authority for the deterministic anti-fraud result.

## 15. Persistence

The first assisted route performs no persistence.

Specifically:

```text
AnalysisRecord writes = 0
LLM prompt persistence = 0
raw provider response persistence = 0
provider reasoning persistence = 0
```

A future explicitly requested history feature requires a separate privacy/data-retention design.

## 16. Privacy disclosure

API documentation must say clearly:

- `/analysis/assisted` sends submitted text and deterministic rule context to the server-configured third-party LLM provider;
- provider retention/data controls depend on the selected provider/account;
- `/analysis/text` and `/analysis/preview` do not invoke the LLM provider;
- provider API credentials remain server-side.

Do not describe the assisted route as local-only.

## 17. Logging

Do not log:

- raw assisted text;
- route Bearer token;
- provider API key;
- Authorization header;
- provider request body;
- provider response body;
- prompt;
- hidden reasoning.

Existing request logger may log method/path/status/duration, but not body or secrets.

## 18. Retry and idempotency statement

The server performs no automatic provider retry.

The first route does **not** claim exactly-once provider execution or exactly-once provider billing across client/network retries.

The Redis rate limiter bounds repeated calls but is not an idempotency mechanism.

Do not add an exactly-once claim unless a later provider-boundary design can prove it.

## 19. Construction and server wiring

When the HTTP transport is enabled, startup wiring conceptually performs:

```text
validate Config
      ↓
resolve selected provider credential
      ↓
NewDefaultRegistry
      ↓
Registry.Create(provider, ProviderConfig{model, key})
      ↓
llmassist.NewService(provider, timeout)
      ↓
register /analysis/assisted with auth + limiter + handler
```

Provider construction performs no external HTTP request.

If construction fails, server startup fails closed.

When transport is disabled, no public assisted route is registered.

## 20. Authorization middleware

Add a small route-specific wrapper around the existing constant-time Bearer primitive, for example:

```go
LLMAssistedAnalysisAuthorization(expectedToken string)
```

Do not duplicate token parsing/constant-time comparison logic.

Auth must execute before rate limiting and before any body/DB/provider work.

## 21. Rate middleware

The first implementation may use a dedicated LLM-assisted rate backend/middleware rather than refactoring the proven rule-submission limiter in the same slice.

Priority is isolation and no regression to the completed rule workflow.

If common rate-limit primitives are extracted later, behavior/key namespaces must remain backward compatible.

## 22. Error mapping

Freeze generic transport errors:

```text
401 unauthorized
429 rate_limited
503 rate_limiter_unavailable
400 invalid_assisted_analysis_request
413 assisted_analysis_request_too_large
415 unsupported_media_type
503 analysis_unavailable    (strict DB load failure)
```

Provider failure after deterministic analysis is not an HTTP transport failure; it is represented inside the 200 response as `llm_assistance.status=unavailable`.

Do not expose provider-specific HTTP codes/bodies to the caller.

## 23. Required negative tests

Every case below must prove provider calls remain zero:

- route disabled;
- missing Authorization;
- malformed Bearer;
- wrong Bearer;
- Redis backend nil;
- Redis error;
- Redis denied/rate exceeded;
- wrong Content-Type;
- oversized HTTP body;
- empty body;
- null/array/scalar JSON;
- unknown field;
- trailing second JSON value;
- missing text;
- blank text;
- text >12 KiB;
- DB query failure.

Where applicable, tests should also prove `AnalysisRecord` writes remain zero.

## 24. Required positive/fallback tests

- valid request returns deterministic result + available assistance;
- deterministic analysis happens before provider invocation;
- exactly one provider call for one successful request;
- provider failure returns HTTP 200 + unchanged deterministic result + unavailable assistance;
- `AnalysisRecord` remains zero;
- response provider/model come from server config;
- request cannot override provider/model;
- route token and provider secret never appear in response/error text.

## 25. Redis integration test

Add real Redis integration coverage using the repository's CI Redis service for the new namespace:

- credential limit enforcement;
- global limit enforcement;
- independent LLM namespace;
- no collision with rule-submission rate keys;
- fail-closed behavior remains covered by unit fakes.

## 26. CI

Full repository gates remain mandatory:

```text
Backend Test
Backend Race (includes llmassist)
Backend Vet
PostgreSQL 16
Redis 7
Frontend Install
Frontend Typecheck
Frontend Build
Extension Test
```

Provider tests continue using fake HTTP only. CI must never require real OpenAI/Gemini/DeepSeek credentials.

## 27. Non-goals

This first transport does not add:

- frontend provider/model settings UI;
- browser-extension LLM call;
- per-request provider/model selection;
- multiple provider profiles;
- arbitrary/custom base URL;
- provider result persistence;
- billing ledger/UI;
- provider retry;
- streaming;
- tool/function calling;
- rule mutation;
- exactly-once billing claim.

## 28. Acceptance summary

```text
Explicit opt-in route only                         YES
Existing text/preview silently call LLM            NO
Independent route token                            YES
Independent Redis rate namespace                   YES
Provider/model caller override                     NO
DB failure before provider call                    YES
Deterministic result remains authority              YES
Provider failure breaks deterministic response      NO
Assisted AnalysisRecord persistence                 NO
Raw text/provider body logging                      NO
Server automatic provider retry                     NO
Exactly-once billing claim                          NO
Real provider calls in CI                           NO
```
