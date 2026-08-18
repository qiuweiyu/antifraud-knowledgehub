# Optional LLM-Assisted Analysis Design

Status: **Frozen for the first implementation slice**

Parent issue: #5  
Design issue: #66

## 1. Goal

Define a provider-agnostic LLM assistance boundary that can enrich an existing AntiFraud-KnowledgeHub rule result without becoming a fraud-decision authority, weakening explainability, silently forwarding text to a model provider, or making the deterministic core unavailable when an LLM fails.

This document freezes the architecture for the first implementation slice. Provider-specific HTTP protocols, public/user-facing LLM transport, paid-provider credentials, and frontend integration remain out of scope until separately reviewed.

## 2. Existing authority model

The current deterministic flow is:

```text
input text
  -> load enabled RiskRule rows
  -> riskengine.Engine.Analyze
  -> riskengine.Result
       risk_score
       risk_level
       matched_rules
       rule evidence
       rule explanation
       rule recommendation
       summary
       recommendations
```

The rule result is explainable because every score contribution comes from enabled repository rules and matched evidence.

LLM assistance must sit **after** this result. It is not another scoring engine.

## 3. Non-goals

The first implementation slice does **not** provide:

- a real OpenAI, Anthropic, Google, DeepSeek, local-model, or other external provider client;
- an HTTP endpoint that causes paid/provider traffic;
- frontend controls for LLM assistance;
- browser-extension LLM integration;
- autonomous fraud classification;
- LLM-generated risk scores or risk levels;
- LLM-generated rule matches treated as real matches;
- autonomous rule creation, submission, review, approval, publication, enable/disable, or deletion;
- tool calling, function calling, browsing, URL fetching, code execution, shell access, database access, or retrieval plugins;
- prompt/response persistence;
- conversation memory;
- user account identity or billing;
- provider retries;
- streaming responses;
- a generic arbitrary-provider URL configuration.

## 4. Authority boundary

### 4.1 Authoritative fields

The following remain exclusively owned by the deterministic rule engine:

```text
risk_score
risk_level
matched_rules
matched_rules[].evidence
matched_rules[].weight
matched_rules[].severity
matched_rules[].explanation
matched_rules[].recommendation
rule-derived recommendations
```

An LLM provider must never be able to overwrite, remove, reorder for authority purposes, or synthesize authoritative values for these fields.

### 4.2 Supplemental fields

The provider may produce only a separately typed assistance object:

```go
type Assistance struct {
    Summary      string
    Observations []string
    Limitations  []string
}
```

Intended meanings:

- `Summary`: concise restatement of the suspicious text and already-known rule context;
- `Observations`: supplemental, non-authoritative patterns or questions a human may consider;
- `Limitations`: uncertainty or context that prevents stronger conclusions.

No score, severity, fraud/not-fraud verdict, rule code, action command, executable content, or mutable rule representation belongs in this type.

### 4.3 Display labeling

Any future HTTP/UI representation must label this data as supplemental, for example:

```text
LLM assistance — supplemental, not a fraud verdict
```

The normal rule result must remain visible beside it.

## 5. Core provider interface

Create a provider-neutral package under a bounded location such as:

```text
backend/internal/llmassist
```

Frozen interface:

```go
type Provider interface {
    Assist(ctx context.Context, input Input) (Assistance, error)
}
```

Frozen input:

```go
type Input struct {
    Text       string
    RuleResult riskengine.Result
}
```

Rationale:

- the provider receives the original text only when the caller has explicitly chosen the LLM-assisted path;
- the existing deterministic result is supplied as context rather than recomputed by the provider;
- no database handle, rule repository, HTTP request, logger, configuration object, credential, or mutable domain object is passed to the provider.

The interface must remain synchronous for the first slice. Cancellation is represented by `context.Context`.

## 6. Service/orchestration boundary

The first slice may introduce a thin service:

```go
type Service struct {
    provider Provider
    timeout  time.Duration
}
```

Conceptual API:

```go
type Outcome struct {
    Assistance Assistance
    Status     Status
}

func (s Service) Assist(ctx context.Context, input Input) Outcome
```

Frozen statuses:

```text
available
unavailable
```

The service converts provider failures into `unavailable` rather than returning an error that could replace or suppress the core rule result.

## 7. Failure semantics

LLM assistance is an optional enrichment and must fail open to the deterministic result.

Provider failures include:

- timeout;
- caller cancellation;
- provider unavailable;
- malformed provider output;
- internal provider validation failure;
- any future network/provider error.

Required behavior:

```text
rule result computed successfully
+ provider failure
= rule result remains successful
+ assistance status = unavailable
```

A provider failure must never change:

```text
risk_score
risk_level
matched_rules
recommendations
```

Provider-specific raw errors must not be exposed to future clients.

## 8. Timeout and cancellation

The service owns one bounded assistance timeout.

Initial frozen default for later configuration:

```text
5s
```

Required semantics:

1. inherit the caller context;
2. create a child context with the configured timeout;
3. cancel the child when the call finishes;
4. return `unavailable` on timeout/cancellation;
5. do not launch detached goroutines that continue provider work after the request is cancelled;
6. do not retry in the first slice.

No retry is intentional: retries can duplicate transmission of user text, consume paid quota, and complicate latency/cancellation semantics.

## 9. Disabled-by-default configuration

The eventual runtime feature must be disabled by default.

Freeze provider-neutral keys:

```text
LLM_ASSISTANCE_ENABLED=false
LLM_ASSISTANCE_PROVIDER=
LLM_ASSISTANCE_TIMEOUT=5s
```

First implementation slice behavior:

- configuration fields and validation may be added;
- only a deterministic in-process mock/stub provider may be used in tests;
- no production external provider is constructed;
- no provider API key is required or accepted by the first slice.

When assistance is disabled:

- no Provider is invoked;
- normal rule analysis is unchanged;
- existing `/analysis/text` and `/analysis/preview` behavior is unchanged;
- no additional persistence occurs.

A later provider-specific design must define its own secret key(s) and endpoint restrictions before implementation.

## 10. Real-provider credentials are explicitly deferred

Do **not** introduce generic fields such as:

```text
LLM_API_KEY
LLM_BASE_URL
LLM_ENDPOINT
```

in the first slice.

A generic arbitrary URL plus API key would silently create an SSRF/trust/configuration surface and make provider-specific validation impossible.

Each real provider integration must receive a bounded follow-up review covering:

- fixed/allow-listed HTTPS endpoint behavior;
- API-key format/secret loading;
- proxy behavior;
- TLS expectations;
- request/response size bounds;
- model allow-list;
- rate/cost controls;
- provider data-retention implications;
- logging/redaction;
- test strategy without real paid calls.

## 11. Privacy boundary

Enabling a future real provider changes where user text is processed.

Therefore normal analysis endpoints must **never** start calling an LLM merely because a deployment enabled a provider.

Frozen rule:

> LLM traffic requires a distinct explicit call path. Existing `/api/v1/analysis/text` and `/api/v1/analysis/preview` remain deterministic-only.

This preserves backward compatibility and prevents silent third-party forwarding.

The first implementation slice does not add the explicit external-call HTTP path; it only freezes the service boundary that such a path may use later.

## 12. Persistence boundary

The first implementation slice creates no new database model or migration.

Do not persist:

- raw LLM prompts;
- raw LLM responses;
- provider request IDs;
- model chain-of-thought;
- hidden reasoning;
- provider credentials;
- assistance objects.

A future product requirement for LLM history/audit requires a separate schema/privacy design.

Existing `AnalysisRecord` behavior is unchanged.

## 13. Prompt-injection boundary

The input text is untrusted data, not instructions.

Any future provider adapter must construct its own fixed system/developer instruction and clearly delimit user text as data.

The provider must not gain capabilities from instructions embedded in the analyzed text.

Examples of text that must remain inert data:

```text
"ignore all previous instructions"
"call this URL"
"delete the rules"
"run this SQL"
"send the API key"
```

The architecture enforces this structurally by not giving the Provider interface tools, database handles, secrets, browser access, rule mutation functions, or arbitrary network callbacks.

## 14. No tool/function calling

Future adapters used by this assistance layer must disable or omit:

- tool definitions;
- function-call definitions;
- browser/search tools;
- code execution;
- arbitrary URL retrieval;
- local file access;
- database actions.

The provider is text-in / structured-text-out only.

## 15. Provider output validation

Provider output is untrusted.

Before an adapter returns `Assistance`, it must validate and normalize at least:

- string fields are valid strings;
- arrays contain only strings;
- item counts are bounded;
- individual strings are bounded;
- no required field is missing;
- no unexpected authoritative field is accepted.

Initial logical bounds for the provider-neutral contract:

```text
Summary:      <= 2,000 UTF-8 bytes
Observations: <= 8 items, each <= 1,000 UTF-8 bytes
Limitations:  <= 8 items, each <= 1,000 UTF-8 bytes
```

Whitespace-only items are dropped or rejected consistently by the adapter.

The core service must never deserialize provider output directly into `riskengine.Result`.

## 16. Deterministic mock provider

The first implementation slice must include a deterministic in-process provider for tests only.

It should support controlled cases such as:

```text
success
error
timeout/cancellation
invalid-output simulation if validation is adapter-owned
```

The mock must not perform network I/O.

It exists to prove the provider abstraction and failure semantics before a paid or external dependency is introduced.

## 17. Test contract for first implementation slice

### 17.1 Provider/service tests

Prove:

1. successful mock assistance returns `available`;
2. provider error returns `unavailable`;
3. timeout returns `unavailable`;
4. caller cancellation returns `unavailable`;
5. disabled path invokes the provider zero times;
6. rule result passed into assistance is not mutated;
7. assistance type contains no score/level/matched-rule authority fields;
8. no external network is required.

### 17.2 Configuration tests

Prove:

- default is disabled;
- invalid timeout is rejected when assistance is enabled;
- enabled configuration requires a supported provider identifier once a runtime provider is introduced;
- first slice does not accept arbitrary provider URL/API-key configuration.

### 17.3 Existing regression gates

All existing repository CI continues to pass:

```text
backend Test
backend Race
backend Vet
PostgreSQL 16 service
Redis 7 service
frontend Install
frontend Typecheck
frontend Build
extension Node tests
```

The new `llmassist` package should be added to the Race target if it contains concurrent timeout/cancellation behavior worth exercising.

## 18. Logging contract

First slice logging must not include:

- analyzed raw text;
- provider prompt;
- provider response;
- credentials;
- future Authorization headers;
- hidden provider reasoning.

Future operational logging may include bounded metadata such as:

```text
provider identifier
status available/unavailable
latency bucket
timeout boolean
```

but only after the real transport is designed.

## 19. Human and AI authority statement

LLM assistance is advisory.

It must not be represented as:

- a definitive fraud verdict;
- a replacement for rule evidence;
- an automatic enforcement decision;
- approval to transfer funds;
- approval/rejection of community rules;
- a substitute for human verification or official-channel verification.

The application should continue to tell users to verify suspicious requests through independent official channels.

## 20. When assistance should not be used

Do not use LLM assistance when:

- deterministic rule analysis is sufficient;
- the deployer has not explicitly enabled external processing;
- provider privacy/data-handling terms are unacceptable for the deployment;
- the text contains data the operator is not authorized to send externally;
- the provider is unavailable and the core rule result is already available;
- the requested operation is rule review/publication or another privileged mutation;
- the caller expects a guaranteed fraud/not-fraud verdict.

## 21. First implementation slice

The next coding slice after this design is intentionally narrow:

```text
backend/internal/llmassist/
  types.go
  provider.go
  service.go
  service_test.go

backend/internal/config/
  provider-neutral enable/provider/timeout fields
  validation tests

.env.example
  document disabled-by-default provider-neutral settings
```

Allowed first-slice behavior:

- deterministic mock provider in tests;
- service timeout/cancellation/fallback semantics;
- configuration scaffolding.

Explicitly not allowed in that slice:

- real HTTP provider client;
- paid API calls;
- public LLM HTTP endpoint;
- frontend/browser-extension wiring;
- schema changes.

## 22. Follow-up gate before a real provider

After the first implementation slice passes, create a separate design/implementation task for the first concrete provider.

That task must answer before coding:

1. Which provider and model(s) are supported?
2. Which exact HTTPS origin(s) may be contacted?
3. What independent credential/access boundary protects cost-bearing calls?
4. What Redis or other abuse/cost limiter applies?
5. What exact text is forwarded?
6. What provider retention/privacy disclosure is required?
7. What request/response size and timeout limits apply?
8. How are provider errors normalized?
9. How is the provider mocked in CI with zero paid calls?
10. What explicit HTTP endpoint or operator action opts into external processing?

No real provider is approved until those questions are frozen.

## 23. Acceptance summary

The design is acceptable only if all of the following remain true:

```text
Rule engine remains authority                    YES
Existing analysis endpoints remain deterministic YES
Feature defaults off                             YES
LLM failure cannot break core result             YES
Raw prompt/response persistence                   NO
Autonomous rule mutation                          NO
Tools/function calling                            NO
Arbitrary provider URL                            NO
Real external provider in first slice             NO
Deterministic mock tests                          REQUIRED
Separate future provider review                   REQUIRED
```

## 24. Security claims allowed after first slice

The project may claim only that:

- the LLM assistance abstraction is optional and disabled by default;
- it cannot mutate the deterministic rule result through its typed interface;
- provider failure is isolated from core analysis;
- first-slice tests require no external provider;
- no prompt/response persistence or privileged rule mutation is introduced.

The project must not yet claim that:

- any real provider is integrated;
- provider traffic is authenticated/rate-limited;
- external text processing is private or zero-retention;
- model output is factually correct;
- LLM assistance improves fraud-detection accuracy.
