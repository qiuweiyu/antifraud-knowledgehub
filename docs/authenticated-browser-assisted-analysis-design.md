# Authenticated Browser Bridge for Assisted Analysis

Status: **FROZEN DESIGN**  
Parent: #5  
Design issue: #92  
Base main: `e1059136eb3503305c0fb23028ec15c963afa6d4`

## 1. Purpose

This document freezes the first browser-safe authorization boundary for cost-bearing AI-assisted analysis.

The current Vue application is a public SPA with no authenticated browser principal. The existing controlled `POST /api/v1/analysis/assisted` transport is protected by a privileged server/operator Bearer credential and MUST NOT be called by placing that credential in browser code, `VITE_*`, browser storage, HTML, source maps or the browser extension.

The selected first slice is deliberately **controlled beta access**, not anonymous public LLM access and not a general account/RBAC platform.

The browser will authenticate with an operator-provisioned high-entropy **Browser Access Grant**. The server will exchange a valid grant for a short-lived opaque HttpOnly Redis-backed session. Cost-bearing browser requests will authorize from that server-resolved session principal and will use only server-owned Assisted AI Profiles.

The deterministic risk engine remains the only risk-decision authority. LLM output remains supplemental.

## 2. Existing contracts that remain authoritative

The following live-main contracts remain unchanged:

- `POST /api/v1/analysis/text` remains deterministic analysis and may persist its existing analysis history.
- `POST /api/v1/analysis/preview` remains deterministic-only and no-write.
- `POST /api/v1/analysis/assisted` remains the existing controlled operator transport.
- The existing assisted route keeps its independent privileged Bearer credential and strict `{ "text": "..." }` request body.
- The existing assisted route is disabled by default.
- Provider/model/API credentials remain server-owned.
- The Assisted AI Profile Registry is server-owned and resolves only approved profile IDs.
- Deterministic `rule_result` remains authoritative.
- Provider failure after deterministic success may return supplemental `unavailable` while preserving the deterministic result.
- There is no automatic provider retry and no exactly-once execution or billing guarantee.
- Provider calls transfer submitted text and deterministic context to the selected third-party provider.

The browser bridge MUST be a separate contract. It MUST NOT silently broaden or weaken `/api/v1/analysis/assisted`.

## 3. Current security finding

The public SPA cannot safely possess the existing assisted-analysis Bearer token. A browser-readable static token is not a server secret.

CORS is not authentication. An allowed origin alone does not establish a trusted caller and cannot protect a cost-bearing provider credential.

Anonymous browser access also has no stable server-resolved principal for per-caller cost control. Therefore direct Vue access to the current controlled assisted route remains stopped.

## 4. Selected architecture

### 4.1 Controlled Browser Access Grant

The first browser principal is an operator-provisioned, high-entropy access grant.

Each configured grant maps to exactly one server-owned principal:

```text
BrowserAccessGrantRecord {
  principal_id          stable opaque server-owned identifier
  principal_generation positive bounded revocation generation
  display_label         optional non-authoritative label
  grant_digest          SHA-256 digest of a high-entropy bearer grant
  enabled               boolean
}
```

Required invariants:

- raw grants are never embedded in the SPA, browser extension, repository or generated frontend assets;
- the server stores/configures only the digest needed for verification, not the raw grant;
- a raw grant contains at least 256 bits of cryptographic randomness before encoding;
- principal IDs are bounded, normalized and safe to log as identifiers;
- principal generation is a positive bounded server-owned value and is never supplied by the browser;
- duplicate principal IDs or duplicate grant digests fail startup;
- disabled or malformed grant records fail closed;
- configured grant count is bounded;
- raw grants, grant digests and authentication failures are never written to application logs;
- grant verification uses fixed-length digest comparison and does not expose match details;
- the grant is an end-user/browser authentication credential, never a provider API key and never the existing controlled assisted-analysis Bearer token.

The raw grant may be entered by the user into an explicit unlock/login form and sent once to the session-exchange endpoint over HTTPS. The application MUST NOT persist the raw grant in `localStorage`, `sessionStorage`, IndexedDB, Pinia persistence, cookies, URLs or source maps.

This controlled grant mechanism is intentionally narrower than public signup. A future general identity system requires a separate design and may replace grant bootstrap while preserving the session/authorization boundary.

### 4.2 Opaque browser session

On successful grant verification the server mints a cryptographically random opaque session token with at least 256 bits of entropy.

The raw session token is sent only in an HttpOnly cookie. Server-side Redis state is keyed by a one-way digest of the token, not by the raw cookie value.

Conceptual server-side state:

```text
BrowserSession {
  principal_id
  principal_generation
  issued_at
  expires_at
}
```

Frozen first-slice semantics:

- absolute lifetime: 1 hour;
- no sliding expiration;
- browser close may end the client cookie earlier;
- logout deletes Redis session state and clears the cookie;
- every session validation re-resolves the current server-owned principal and requires the stored `principal_generation` to match;
- removing/disabling a principal or incrementing its generation invalidates its previously issued sessions after the updated server configuration is deployed;
- a process restart with unchanged principal configuration does **not** invalidate otherwise-valid sessions;
- sessions are not bound to one process instance and may be validated by another instance that shares Redis and the same principal registry;
- session IDs are never returned in JSON and never logged;
- Redis unavailable/error/timeout during session validation fails closed;
- no database-backed session table is introduced.

This avoids process-local session affinity while retaining explicit operator revocation. Multi-instance deployments MUST roll the same principal configuration/generation across instances before claiming revocation is complete.

Production cookie contract:

- name: `__Host-afkh_browser_session`;
- `HttpOnly`;
- `Secure`;
- `SameSite=Strict`;
- `Path=/`;
- no `Domain` attribute.

The production browser bridge requires HTTPS. A development-only loopback mode may use an explicitly separate non-production cookie configuration, but it MUST be impossible to enable insecure cookies in production mode.

## 5. Same-origin and Origin boundary

The first browser bridge supports a single configured canonical browser origin and is designed for same-origin deployment through a reverse proxy/BFF-style path layout.

Examples:

```text
https://antifraud.example/
https://antifraud.example/api/v1/...
```

Development may use a Vite proxy so the browser still sees one origin while `/api` is forwarded to the Go backend.

Requirements:

- bridge configuration contains exactly one canonical browser origin;
- state-changing browser-bridge requests require an exact `Origin` match to that canonical origin;
- wildcard, suffix, substring and reflected-origin matching are forbidden;
- missing/malformed/unexpected Origin fails closed for browser session exchange, logout and cost-bearing assisted requests;
- generic repository CORS configuration does not authorize the browser bridge;
- the bridge does not rely on CORS for authentication.

Cross-site cookie deployments are out of scope for the first slice and require a separate design.

## 6. CSRF contract

Cookie authentication introduces CSRF risk even with `SameSite=Strict`, so state-changing bridge endpoints also require a per-session CSRF token.

The CSRF token is deterministically derived from the high-entropy raw session token using a domain-separated one-way SHA-256 construction, for example conceptually:

```text
csrf_token = base64url(SHA-256("afkh-browser-csrf-v1\x00" || raw_session_token))
```

The raw session token remains HttpOnly and is never returned to JavaScript. The derived CSRF token does not permit recovery of the 256-bit session token and does not authenticate a request without the session cookie.

The browser keeps the CSRF token in application memory and sends it as:

```text
X-AFKH-CSRF: <token>
```

A session-status endpoint may recompute and return the CSRF token for an already valid session so a page reload can restore in-memory state. Cross-origin callers cannot read that response under the browser same-origin policy, and exact Origin checks still apply to state-changing requests.

Requirements:

- CSRF derivation is domain-separated from Redis/session-key hashing;
- the derived token has a fixed 256-bit digest before encoding;
- comparison is constant-time over fixed-length decoded bytes;
- CSRF failure returns before analysis DB work or provider invocation;
- CSRF token is never logged or persisted by the frontend;
- provider/API credentials are never used as CSRF material.

## 7. Session HTTP contract

### 7.1 Exchange access grant for session

```http
POST /api/v1/browser/session/exchange
Content-Type: application/json
Origin: <exact configured browser origin>

{
  "access_grant": "..."
}
```

Contract:

1. bridge feature enabled check;
2. exact Origin check;
3. exact JSON content type;
4. small bounded request body;
5. strict single-field JSON decode;
6. pre-auth Redis abuse limiter by coarse network source plus global limit;
7. grant digest verification against the server registry;
8. mint a session value with cryptographic randomness and derive its CSRF token;
9. persist bounded session state in Redis with absolute TTL;
10. set HttpOnly session cookie;
11. return only non-secret session metadata plus the derived CSRF token;
12. `Cache-Control: no-store`.

The coarse network source MUST come from the direct peer address by default. Forwarded client-IP headers may be trusted only behind an explicitly configured trusted-proxy boundary; an arbitrary `X-Forwarded-For` value from an untrusted peer MUST NOT bypass the exchange limiter.

Authentication failure MUST use a generic response and MUST NOT reveal whether a principal exists, a grant is disabled, or a digest was close to matching.

### 7.2 Read current session

```http
GET /api/v1/browser/session
```

Requires a valid session cookie. Returns only bounded non-secret session metadata and the recomputed CSRF token for the current valid session. It MUST NOT return the raw session ID, access grant or any provider credential.

### 7.3 Logout

```http
POST /api/v1/browser/session/logout
X-AFKH-CSRF: <token>
Origin: <exact configured browser origin>
```

Requires valid session + exact Origin + CSRF. Deletes the Redis session and clears the cookie. Logout is idempotent from the user's perspective.

All session responses use `Cache-Control: no-store`.

## 8. Browser profile metadata contract

After session authentication, the browser may fetch only the existing bounded public Assisted AI Profile metadata:

```http
GET /api/v1/browser/analysis/assisted/profiles
```

Response elements are limited to the frozen `ProfilePublicMetadata` surface:

- `id`;
- `display_name`;
- `provider_display_name`;
- `model_display_name`;
- `availability`;
- `disclosure`.

The endpoint MUST NOT expose API keys, route Bearer tokens, raw provider request configuration, custom base URLs, internal endpoints, hidden prompts, Redis keys or billing credentials.

The endpoint requires a valid browser session even though the DTO itself is non-secret. This avoids exposing deployment-specific enabled-profile metadata to unauthenticated callers in the first controlled slice.

## 9. Browser assisted-analysis HTTP contract

The browser receives a new endpoint. The existing controlled route remains unchanged.

```http
POST /api/v1/browser/analysis/assisted
Content-Type: application/json
Origin: <exact configured browser origin>
X-AFKH-CSRF: <token>

{
  "text": "...",
  "profile_id": "default"
}
```

Request rules:

- strict JSON object; unknown fields rejected;
- bounded request body;
- text keeps the existing assisted-analysis 12 KiB maximum and is never silently truncated;
- `profile_id` is required, bounded and resolved only through the server-owned Profile Registry;
- browser cannot supply raw provider, model, API key, base URL, endpoint, timeout, output-token budget, tools, retry policy or provider options.

Frozen execution order:

1. bridge feature enabled check;
2. exact Origin check;
3. session cookie validation in Redis, including current principal/generation validation;
4. CSRF validation;
5. per-principal + global cost/rate admission in Redis;
6. exact content type and bounded strict request decode;
7. resolve `profile_id` through the server-owned Profile Registry;
8. strict enabled-rule database load with explicit DB error handling;
9. deterministic risk engine analysis;
10. exactly one selected server-side profile assistance call;
11. return authoritative deterministic result plus supplemental assistance/profile display metadata;
12. `Cache-Control: no-store`.

No rejected path before step 9 may invoke a provider. DB load failure also performs zero provider calls.

Provider failure after deterministic success keeps HTTP success semantics compatible with the existing assisted route: deterministic `rule_result` remains unchanged and the AI section reports `status=unavailable`.

No automatic retry is introduced.

## 10. Cost and abuse controls

The existing operator-route credential limiter is not reused as browser identity.

Browser cost controls use a separate Redis namespace and server-resolved principal ID.

At minimum the implementation MUST provide:

- pre-auth session-exchange limiter by coarse network source;
- session-exchange global limiter;
- assisted-analysis per-principal limiter;
- assisted-analysis global limiter;
- bounded fixed time window(s);
- atomic Redis admission for per-principal/global assisted counters;
- fail-closed behavior for Redis nil/error/timeout;
- no client-controlled limit, timeout, retry or output-budget escalation;
- one selected profile/provider invocation maximum per accepted request.

Per-session rate limiting alone is insufficient because one principal may obtain multiple sessions. Cost admission is keyed by the server-owned principal.

The system MUST NOT claim exact monetary accounting, exactly-once provider execution or exactly-once billing.

## 11. Secret, privacy and logging boundary

The browser bridge MUST never expose or log:

- provider API keys;
- `LLM_ASSISTED_ANALYSIS_TOKEN`;
- raw access grants;
- access-grant digests;
- raw browser session IDs;
- CSRF tokens;
- raw Redis keys containing credentials;
- provider authorization headers;
- hidden prompts or raw provider error bodies.

Submitted suspicious text and provider raw responses MUST NOT be written to new bridge logs or persistence.

The browser UI must show the selected profile's third-party-transfer disclosure immediately adjacent to the explicit assisted action. Deterministic analysis must remain usable without enabling the assisted flow.

## 12. Failure semantics

The browser must be able to distinguish at least:

- unauthenticated/expired/revoked session;
- CSRF or Origin rejection;
- session/cost-control Redis unavailable;
- rate/cost admission denied;
- invalid request;
- unknown/disabled/unavailable profile;
- deterministic analysis/database failure;
- provider unavailable after deterministic success.

Security-sensitive errors remain generic and secret-safe.

Recommended transport classes:

- `401` invalid/expired/revoked browser session;
- `403` Origin/CSRF authorization failure;
- `400` bounded request/profile validation failure;
- `429` rate/cost denial;
- `503` required Redis/session/cost infrastructure unavailable;
- existing server error class for strict rule-load/database failure;
- `200` when deterministic analysis succeeds but provider assistance is unavailable.

## 13. CI and test contract

Implementation CI MUST perform zero real OpenAI, Gemini or DeepSeek calls and require zero real provider credentials.

Required proof includes:

- malformed/disabled grant registry fails closed;
- duplicate principal/digest and invalid principal generation fail closed;
- grant comparison does not expose raw secret material;
- invalid grant cannot mint a session;
- exact Origin enforcement;
- untrusted forwarded-IP headers cannot bypass pre-auth rate limits;
- production cookie flags;
- session-token randomness through an injectable test seam without weakening production entropy;
- CSRF derivation is stable, session-bound and non-reversible by construction;
- session expiry/logout/principal-generation revocation;
- sessions are not process-instance-affine when instances share Redis and principal configuration;
- Redis failure fails closed;
- CSRF failure causes zero analysis DB work and zero provider calls;
- unauthenticated request causes zero analysis DB work and zero provider calls;
- rate/cost denial causes zero analysis DB work and zero provider calls;
- invalid profile causes zero provider calls;
- DB failure causes zero provider calls;
- successful request invokes exactly one selected server-owned profile;
- provider failure preserves the deterministic result;
- public profile/session responses contain no configured secrets;
- existing `/api/v1/analysis/assisted` request/authorization contract remains unchanged;
- Backend Test, Race, Vet, PostgreSQL/Redis integration, Frontend typecheck/build and Extension tests remain green.

All provider behavior tests use injected fake HTTP transport.

## 14. Implementation sequence

### AI-BROWSER-I1A — Access grant and session foundation

Scope:

- bounded server-owned grant registry with explicit principal generation;
- Redis-backed opaque session store;
- cryptographic session-token generation and domain-separated CSRF derivation;
- exact-Origin middleware;
- session exchange/status/logout endpoints;
- trusted-peer-aware pre-auth exchange abuse limiter;
- default-off configuration;
- no provider call and no Vue assisted UI.

### AI-BROWSER-I1B — Authenticated assisted-analysis bridge

Scope:

- session-protected public profile metadata endpoint;
- separate `POST /api/v1/browser/analysis/assisted` contract;
- per-principal + global fail-closed cost limiter;
- server-owned `profile_id` resolution;
- deterministic-first execution;
- zero-provider-call negative-path proofs;
- reuse existing provider/service implementations with fake HTTP in CI;
- existing controlled `/analysis/assisted` unchanged.

### AI-UX-I1 — Explicit Vue assisted-analysis UX

Only after I1A + I1B are green:

- explicit access-grant/session unlock flow;
- keep grant only in transient form state until exchange completes;
- keep CSRF token in memory;
- explicit assisted opt-in;
- third-party transfer disclosure adjacent to action;
- deterministic result visually primary;
- supplemental/unavailable AI states;
- no browser API keys or privileged controlled-route token.

### AI-UX-I2 — Optional approved profile selector

Only when more than one server-approved profile is intentionally available.

The browser selects only `profile_id`.

## 15. Non-goals

This design does not introduce:

- anonymous public cost-bearing LLM access;
- public registration;
- password database;
- OAuth/OIDC/social login;
- passkeys/WebAuthn;
- general RBAC or community contributor identity;
- database-backed session/profile/grant CRUD;
- browser-extension LLM access;
- provider adapter protocol changes;
- arbitrary client provider/model/base URL selection;
- streaming, tools, function calling or automatic retries;
- billing guarantees.

Those require separate bounded designs if product need justifies them.

## 16. Stop conditions

STOP implementation and return to design review if a proposed change requires or introduces:

- the existing privileged assisted-analysis Bearer token in browser-readable code/storage;
- provider API keys in frontend code or browser storage;
- a raw Browser Access Grant persisted by the application after exchange;
- localStorage/sessionStorage/IndexedDB session credentials;
- CORS as the primary authorization control;
- wildcard/reflected Origin authorization;
- cross-site session cookies in the first slice;
- trust in arbitrary forwarded client-IP headers;
- process-local session affinity as an authorization requirement;
- client-supplied provider/model/base URL/endpoint/credential/options;
- provider invocation before authorization, cost admission and deterministic analysis;
- LLM output modifying risk score, risk level, matched rules, publication or rule state;
- Redis fail-open session/cost behavior;
- silent changes to `/api/v1/analysis/assisted`;
- mutable database grant/session administration without a separate permission/audit design.

## 17. Decision summary

**PASS for AI-BROWSER-D1 design.**

The selected first browser bridge is a controlled, same-origin, operator-provisioned access-grant flow that exchanges a high-entropy browser credential for a short-lived opaque HttpOnly Redis session with exact-Origin, CSRF and per-principal/global cost controls.

Session revocation is principal-generation based rather than process-instance based, so the design does not require sticky sessions or invalidate valid sessions merely because a server process restarted.

It does not create anonymous public LLM access and does not export any privileged server/provider credential to the SPA.

The next bounded implementation task is **AI-BROWSER-I1A: Access grant and session foundation**. No Vue cost-bearing assisted call may ship until AI-BROWSER-I1A and AI-BROWSER-I1B are green.
