# Assisted Analysis Profile Selection and Frontend Trust Boundary

Status: **FROZEN DESIGN**  
Parent: #5  
Design issue: #88  
Base main: `e80fc9162e766ad924091f464e718971b00c95a5`

## 1. Purpose

This document freezes the next safe product boundary after the explicit server-side assisted-analysis transport became green.

The goal is to support a future user-facing AI-assisted analysis experience and future choice between approved AI configurations **without exporting privileged server credentials to the browser and without allowing clients to route arbitrary provider traffic**.

This is a design-only slice. It does not change runtime behavior.

## 2. Existing contracts that remain authoritative

The following contracts are already delivered and MUST remain unchanged unless a later design explicitly supersedes them:

- `/api/v1/analysis/text` remains deterministic rule analysis and may persist analysis history according to its existing behavior.
- `/api/v1/analysis/preview` remains deterministic-only and no-write.
- `/api/v1/analysis/assisted` is an explicit cost-bearing controlled route.
- The assisted route is disabled by default.
- The assisted route uses an independent server-configured Bearer credential and independent fail-closed Redis limits.
- The current assisted request body is strict `{ "text": "..." }`.
- The server owns the configured provider and model.
- Provider credentials remain server-only.
- Deterministic `rule_result` is authoritative; LLM output is supplemental.
- Provider failure after deterministic success may produce `llm_assistance.status=unavailable` while preserving the deterministic result.
- No provider retry or exactly-once billing guarantee exists.
- Assisted input is explicitly transferred to the configured third-party provider when an assisted call is made.

This design MUST NOT silently weaken those contracts.

## 3. Current frontend security reality

The current Vue application is a public browser SPA. Its Axios client configures only a base URL and timeout. The repository currently has no end-user login, server-issued browser session, OAuth flow, RBAC layer, BFF identity boundary, or equivalent authenticated browser principal.

Therefore:

> A privileged assisted-analysis Bearer token cannot safely be placed in a `VITE_*` environment variable, JavaScript bundle, local storage, session storage, IndexedDB, HTML, source map, browser extension bundle, or browser-readable cookie.

A browser-delivered static token is public to the browser user and cannot be treated as a server secret.

CORS is not authentication or authorization. Restricting origins does not make a browser-bundled secret safe and does not establish a trusted caller identity.

## 4. Design finding

The previous roadmap phrase “Frontend provider/model selection” is too broad to implement directly against the current transport.

Two independent conflicts exist:

1. **Credential conflict** — the public SPA has no safe way to hold the current controlled assisted-analysis Bearer credential.
2. **Routing conflict** — the current assisted transport deliberately prohibits client-supplied provider/model selection and current runtime configuration selects one provider/model per process.

Direct Vue implementation is therefore **STOPPED** until the server-side trust and selection boundaries described below exist.

## 5. Selected architecture: server-owned Assisted AI Profiles

A browser MUST NOT select a raw provider name, raw model identifier, API key, provider token, base URL, endpoint URL, tool set, retry policy, timeout override, or arbitrary provider options.

Instead, future browser-facing selection uses a server-owned **Assisted AI Profile**.

Conceptual public metadata:

```text
AssistedAIProfilePublic {
  id                    stable opaque profile identifier
  display_name          human-readable profile label
  provider_display_name human-readable third-party provider label
  model_display_name    human-readable model label
  availability          available | unavailable | disabled
  disclosure            bounded third-party-transfer disclosure
}
```

Conceptual server-only resolution:

```text
AssistedAIProfileRuntime {
  public_metadata
  provider_factory / provider instance
  server-owned model identifier
  server-owned provider credential reference
  timeout / bounded runtime policy
  cost / rate policy class
}
```

The exact Go types and configuration source belong to a later implementation issue, but the trust split above is frozen.

### 5.1 Profile ID requirements

A future `profile_id` is a routing alias controlled by the server, not a model name supplied by the client.

It MUST:

- be bounded and validated;
- resolve only through the server registry;
- fail closed when unknown, disabled, ambiguous or misconfigured;
- never be interpreted as a URL, provider name, model path, file path or expression;
- never contain embedded credentials;
- remain safe to log as a non-secret identifier.

### 5.2 Public metadata requirements

Public/display metadata MAY expose:

- stable profile ID;
- human display label;
- provider display name;
- model display label;
- availability state;
- third-party transfer disclosure.

It MUST NOT expose:

- API keys or provider tokens;
- the controlled assisted-analysis Bearer token;
- secret references;
- provider authorization headers;
- arbitrary/custom base URLs;
- internal network addresses;
- raw provider request templates;
- hidden system prompts;
- Redis keys or credential digests;
- billing credentials.

## 6. Browser execution boundary

### 6.1 Current controlled route

`POST /api/v1/analysis/assisted` remains a server/operator controlled transport.

The public Vue SPA MUST NOT call it by embedding its Bearer credential.

### 6.2 Future browser-facing bridge

Before a user-facing Vue assisted-analysis action can be enabled, the backend must possess a server-resolved browser principal or another non-exportable authorization boundary.

Acceptable future designs may include a server-issued authenticated session or another explicitly designed BFF/auth mechanism. The exact identity product is not frozen here.

Whatever mechanism is chosen MUST provide:

- server-side authorization decision before cost-bearing work;
- a caller identity or policy principal that cannot be forged by merely editing browser JavaScript;
- fail-closed abuse/cost rate limiting tied to that server-resolved principal and a global budget;
- no privileged static credential in browser-readable code or storage;
- CSRF protection if cookie-based authentication is selected;
- explicit session lifetime/revocation semantics if sessions are selected;
- no reliance on CORS as the authorization control.

Until this bridge exists, the public SPA remains deterministic-only.

## 7. Future profile selection request contract

The existing strict `{ "text": "..." }` contract is not modified by this design.

A future browser-facing assisted contract, if approved by a separate design/implementation issue, may accept a bounded server-owned alias such as:

```json
{
  "text": "...",
  "profile_id": "balanced-analysis"
}
```

This example is **not yet an implemented endpoint contract**.

If introduced later:

1. authentication/authorization MUST run first;
2. rate/cost admission MUST run before body/provider work;
3. strict JSON/body/text limits remain required;
4. `profile_id` MUST resolve through the server registry;
5. deterministic rules MUST run before the provider;
6. exactly one selected server-side profile may be invoked;
7. provider failure MUST NOT replace or rewrite deterministic evidence;
8. no raw provider/model/base URL override may be accepted.

A separate endpoint/version is preferred over silently broadening the already-frozen controlled `/analysis/assisted` contract.

## 8. Profile registry invariants

A future registry implementation MUST be validated at startup or construction time.

Required invariants:

- duplicate profile IDs are rejected;
- empty/invalid IDs are rejected;
- profile count is bounded;
- each enabled profile resolves to exactly one registered provider implementation;
- each enabled profile has a valid bounded model identifier according to its provider contract;
- each enabled profile has its required server credential;
- no profile can configure an arbitrary client-controlled base URL;
- invalid enabled profiles fail startup rather than becoming partially live;
- disabled profiles cannot be executed;
- profile public metadata is derived from trusted server configuration, never from request fields.

The initial implementation SHOULD prefer static server configuration over a database-backed mutable catalog. A database/admin mutation system would create a new security and audit surface and requires its own design.

## 9. UX contract

The future Analysis page must keep deterministic analysis visually primary.

Recommended hierarchy:

```text
Input text
  ↓
Deterministic Risk Analysis
  - score
  - risk level
  - matched rules
  - evidence
  - recommendations

Optional AI Assistance
  - explicit opt-in control
  - selected approved profile label
  - third-party provider/model display label
  - third-party data-transfer disclosure
  - supplemental summary / observations / limitations
  - explicit unavailable state
```

The UI MUST NOT:

- describe LLM output as a fraud verdict;
- hide deterministic rule evidence when AI assistance is available;
- imply that provider availability changes the deterministic risk score;
- silently send text to a provider because a page loaded;
- auto-enable assisted mode without an explicit user action;
- expose or ask the user to manage server API keys in the public analysis page.

## 10. Third-party data-transfer consent/disclosure

Before the first cost-bearing assisted request in a user interaction, the UI must clearly disclose that the submitted text will be sent to the selected third-party AI provider.

The disclosure must be associated with the assisted action, not buried only in general documentation.

The deterministic action must remain available without accepting third-party transfer.

The design does not claim that the provider stores or does not store data; provider-specific retention statements require separately verified provider policy/documentation and must not be invented by the application.

## 11. Error and fallback UX

The UI must distinguish:

- deterministic API failure;
- assisted authorization/session failure;
- rate/cost denial;
- provider unavailable fallback;
- invalid/disabled profile;
- ordinary validation errors.

When deterministic analysis succeeded but LLM assistance is unavailable, the page should continue to display the deterministic result and label the AI section as unavailable rather than treating the whole analysis as failed.

## 12. Cost-control boundary

Profile selection does not create a billing guarantee.

Future cost controls must remain server authoritative. At minimum:

- caller/principal rate limit;
- global rate/cost limit;
- bounded text size;
- bounded provider timeout;
- no automatic retries in the first browser-facing slice;
- one server-resolved profile per accepted call;
- no client-controlled token/output budget escalation unless separately designed and bounded.

The system MUST NOT claim exactly-once provider execution or exactly-once billing.

## 13. Browser extension boundary

The current browser extension remains deterministic-only.

It MUST NOT receive:

- assisted-analysis Bearer credentials;
- provider API keys;
- profile execution credentials.

An extension LLM-assisted flow requires its own explicit opt-in and authorization design after the server browser boundary is mature.

## 14. Implementation sequence

This design freezes the following bounded order.

### AI-PROFILE-I1 — Server-owned Profile Registry and public metadata model

Scope:

- introduce a profile registry abstraction;
- validate profile IDs and startup invariants;
- resolve approved provider/model/service server-side;
- expose only bounded non-secret public metadata through an internal/public DTO as appropriate;
- no Vue assisted call yet;
- no browser credential changes;
- no database-backed profile administration.

Required tests:

- duplicate/unknown/disabled profile failures;
- no secret exposure in public metadata;
- provider/model resolution remains server-owned;
- existing `/analysis/assisted` contract unchanged;
- zero real provider calls in CI.

### AI-BROWSER-D1 — Authenticated browser bridge design

Scope:

- choose and freeze the authenticated browser principal/session/BFF boundary;
- define CSRF/session/revocation semantics as applicable;
- define caller/global rate and cost controls;
- define the future profile-selection HTTP contract;
- explicitly decide whether a new endpoint/version is used.

No user-facing LLM call may ship before this design is approved.

### AI-BROWSER-I1 — Authenticated server bridge implementation

Scope follows AI-BROWSER-D1 only.

Must prove unauthorized browser traffic performs zero provider calls and cannot consume privileged server credentials.

### AI-UX-I1 — Vue explicit assisted-analysis UX

Only after AI-PROFILE-I1 + AI-BROWSER-D1/I1 are green:

- add explicit AI-assisted opt-in;
- render approved profile metadata only;
- show third-party transfer disclosure;
- preserve deterministic result as primary;
- render supplemental/unavailable states;
- never embed API keys or privileged assisted tokens.

### AI-UX-I2 — Optional profile selector

Only if more than one approved server profile is intentionally enabled.

The browser selects only `profile_id`; it never controls raw provider/model/base URL.

## 15. Stop conditions

STOP implementation and return to design review if any proposed slice requires or introduces:

- a privileged static token in `VITE_*` config or browser-readable storage;
- provider API keys in frontend code/config;
- CORS as the primary authorization mechanism;
- arbitrary provider/model/base URL supplied by the client;
- silent modification of the existing `/analysis/assisted` request contract;
- provider invocation before deterministic rule analysis;
- LLM output becoming the fraud verdict or risk-score authority;
- uncontrolled retry/tool/streaming behavior;
- mutable database profile administration without a separate permission/audit design.

## 16. Decision summary

**PASS for design; STOP for immediate Vue provider/model implementation.**

The next implementation task is **AI-PROFILE-I1: server-owned Profile Registry and bounded public metadata model**.

A public browser assisted-analysis call remains blocked until a separate authenticated browser bridge is designed and implemented.
