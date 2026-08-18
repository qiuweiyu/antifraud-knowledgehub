# Configurable Multi-Provider LLM Architecture

Status: **Frozen for implementation sequencing**  
Parent issue: #5  
Design issue: #74

## 1. Goal

Evolve the existing optional LLM assistance implementation from one hard-coded OpenAI runtime configuration into a provider-neutral architecture that can select OpenAI, Gemini, DeepSeek, and future bounded adapters without changing the deterministic anti-fraud core.

This design changes **provider construction and configuration**, not the authority model.

The deterministic rule engine remains the only owner of:

- `risk_score`;
- `risk_level`;
- `matched_rules`;
- matched-rule evidence;
- matched-rule weight/severity;
- rule explanations and rule-derived recommendations.

LLM output remains supplemental only:

```go
type Assistance struct {
    Summary      string
    Observations []string
    Limitations  []string
}
```

## 2. Existing foundation

Current `main` already contains:

- provider-neutral `llmassist.Provider`;
- provider-neutral `llmassist.Service`;
- timeout/cancellation fallback;
- deep-copy isolation of deterministic rule results before provider execution;
- bounded supplemental output validation;
- disabled-by-default LLM configuration;
- a bounded OpenAI Responses adapter;
- fake-HTTP tests with zero real provider calls.

The remaining architectural defect is that runtime configuration currently hard-codes one provider and one OpenAI model.

## 3. Architecture target

```text
                         AntiFraud analysis
                               |
                    deterministic RiskResult
                               |
                       llmassist.Service
                               |
                     llmassist.Provider
                               |
                       Provider Registry
                +--------------+--------------+
                |              |              |
             OpenAI         Gemini         DeepSeek
             Adapter        Adapter         Adapter
                |              |              |
            fixed API       fixed API       fixed API
             origin          origin          origin
```

The business/orchestration layer must not contain provider-specific HTTP protocol logic.

## 4. Common runtime configuration

Freeze the common settings as:

```text
LLM_ASSISTANCE_ENABLED=false
LLM_ASSISTANCE_PROVIDER=
LLM_ASSISTANCE_MODEL=
LLM_ASSISTANCE_TIMEOUT=5s
```

Meaning:

- `LLM_ASSISTANCE_ENABLED`: global feature gate;
- `LLM_ASSISTANCE_PROVIDER`: selected provider identifier;
- `LLM_ASSISTANCE_MODEL`: selected provider model identifier;
- `LLM_ASSISTANCE_TIMEOUT`: one bounded end-to-end provider timeout owned by `llmassist.Service`.

Provider choice and model are runtime configuration, not compile-time model constants.

The first multi-provider runtime supports **one selected provider/model per process**. The Registry may know multiple provider factories, but only the configured provider is constructed. A future UI/catalog that keeps multiple simultaneously configured model profiles is a separate product/configuration design.

## 5. Provider identifiers

Initial registered provider identifiers:

```text
openai
gemini
deepseek
```

Provider identifiers are normalized only by trimming surrounding whitespace. Configuration should use exact lower-case identifiers.

Unknown providers fail startup validation when LLM assistance is enabled.

## 6. Model configuration policy

`LLM_ASSISTANCE_MODEL` is operator-configurable.

The common layer validates only generic safety properties:

- valid UTF-8;
- trimmed non-empty value;
- no ASCII control characters;
- maximum 128 UTF-8 bytes.

The common layer **must not** maintain a hard-coded global list of every current provider model. Model catalogs and deprecations change independently from this repository.

Provider adapters may impose additional syntax checks required for safe request construction.

Examples of current provider model IDs may appear in `.env.example` comments or documentation, but examples are not a permanent allowlist.

Unsupported, unavailable, deprecated, or capability-incompatible model IDs fail either:

1. during provider construction if the adapter can prove invalidity locally; or
2. as a normalized provider error at request time.

Provider errors still degrade to `assistance=unavailable` and never alter the deterministic rule result.

## 7. Provider-specific secrets

Keep credentials provider-specific:

```text
OPENAI_API_KEY=
GEMINI_API_KEY=
DEEPSEEK_API_KEY=
```

Do not replace these with one global `LLM_API_KEY`.

Rationale:

- avoids accidentally sending an OpenAI credential to Gemini or DeepSeek after a provider switch;
- makes secret rotation/provider ownership explicit;
- allows future deployments to provision only the selected provider secret;
- keeps provider-specific validation bounded.

When assistance is disabled, none of the provider secrets are required.

When enabled, only the selected provider credential is required.

## 8. No generic arbitrary base URL

Do not introduce:

```text
LLM_BASE_URL
LLM_ENDPOINT
OPENAI_BASE_URL
GEMINI_BASE_URL
DEEPSEEK_BASE_URL
```

for these production adapters.

OpenAI, Gemini and DeepSeek adapters each own a fixed HTTPS provider origin/endpoint.

This prevents an operator typo or hostile configuration from redirecting a real provider API key to an arbitrary server.

A future `custom`, `openai-compatible`, Ollama, vLLM or enterprise proxy provider requires a separate SSRF/TLS/private-network/credential-forwarding design and is not covered here.

## 9. Provider Registry

Add a provider-neutral Registry in `backend/internal/llmassist`.

Conceptual types:

```go
type ProviderConfig struct {
    Model  string
    APIKey string
}

type Factory func(ProviderConfig) (Provider, error)

type Registry struct {
    factories map[string]Factory
}

func NewRegistry() *Registry
func (r *Registry) Register(name string, factory Factory) error
func (r *Registry) Create(name string, cfg ProviderConfig) (Provider, error)
```

Required semantics:

- empty provider names are rejected;
- nil factories are rejected;
- duplicate registration is rejected;
- unknown provider creation is rejected;
- Registry never logs `ProviderConfig`;
- Registry never persists credentials;
- Registry has no network behavior itself;
- adapters own HTTP behavior;
- the Registry is fully built during startup before concurrent request handling.

The first implementation does not require dynamic runtime registration after server startup.

## 10. Runtime wiring boundary

Provider-specific branching is allowed only in a bounded configuration/credential-resolution layer, not in analysis business logic.

Conceptually:

```text
config.Load/Validate
      |
resolve selected provider credential
      |
build registry with approved factories
      |
registry.Create(provider, ProviderConfig{model, secret})
      |
llmassist.NewService(provider, timeout)
```

`analysis` handlers must continue to depend on deterministic analysis unless a future explicit opt-in assisted-analysis transport is separately implemented.

## 11. OpenAI adapter contract

The existing OpenAI Responses adapter remains the baseline provider implementation.

Changes allowed by the multi-provider refactor:

- move provider construction behind Registry;
- consume `LLM_ASSISTANCE_MODEL` instead of a compile-time single-model allowlist;
- preserve the existing fixed OpenAI Responses endpoint;
- preserve server-side Bearer authentication;
- preserve strict supplemental Structured Outputs;
- preserve `store:false`;
- preserve `background:false`;
- preserve bounded output tokens/body/input;
- preserve redirect rejection;
- preserve no retry/tools/streaming/conversation behavior;
- preserve generic, secret-safe errors.

Removing the one-model hard-code must not weaken endpoint, output-schema, prompt-injection, response-size or secret-safety controls.

## 12. Gemini adapter contract

For this project, Gemini assistance is a one-shot, non-interactive transformation of suspicious text plus an already-computed deterministic rule result.

Use Google Gemini `generateContent` REST behavior for the first adapter rather than adding conversation/agent state.

Provider-owned behavior:

- fixed Google Gemini API origin;
- `x-goog-api-key` authentication;
- model supplied by `LLM_ASSISTANCE_MODEL`;
- model ID is validated before being embedded into the request path;
- model ID may contain only ASCII letters, digits, `.`, `_`, and `-`, with maximum 128 bytes;
- system instruction is server-owned;
- suspicious text is serialized as untrusted data;
- request asks for JSON structured output matching `summary`, `observations`, `limitations`;
- one candidate only;
- bounded output tokens;
- no tools/function calling/search/code execution;
- no streaming;
- no conversation or cached-content identifiers;
- no automatic retry;
- redirects rejected;
- response body bounded before JSON decode;
- malformed/blocked/no-candidate/no-usable-output responses become generic provider errors.

Google documents `generateContent` as appropriate for standard non-interactive generation, API-key authentication through `x-goog-api-key`, and structured JSON output using a JSON Schema.

## 13. DeepSeek adapter contract

Use the fixed DeepSeek OpenAI-style chat-completions API:

```text
POST https://api.deepseek.com/chat/completions
```

Provider-owned behavior:

- server-side Bearer API key;
- model supplied by `LLM_ASSISTANCE_MODEL`;
- current provider model names are documentation examples, not common-layer constants;
- fixed server-owned system prompt;
- suspicious text encoded as user data;
- `response_format={"type":"json_object"}`;
- prompt explicitly requires the exact supplemental JSON shape;
- strict local JSON decode rejects unexpected fields;
- one response choice must yield one usable content object;
- output token/body/input limits remain bounded;
- no tool definitions;
- no streaming;
- no automatic retry;
- redirects rejected;
- raw provider error body never returned to callers.

DeepSeek JSON mode guarantees JSON-form output, not this application's authority boundary. Therefore local strict decoding remains mandatory.

## 14. Common request semantics

Every provider receives conceptually the same domain input:

```go
type Input struct {
    Text       string
    RuleResult riskengine.Result
}
```

Provider-specific request envelopes may differ, but all adapters must preserve:

- suspicious text as untrusted data;
- deterministic rule result as context only;
- no authoritative-rule mutation capability;
- no tools;
- no database/repository/logger/config object passed to the model;
- no arbitrary remote callbacks.

## 15. Common response semantics

All adapters return only:

```go
Assistance{
    Summary,
    Observations,
    Limitations,
}
```

Existing provider-neutral validation remains authoritative for final acceptance:

```text
Summary      <= 2,000 UTF-8 bytes
Observations <= 8 items, each <= 1,000 UTF-8 bytes
Limitations  <= 8 items, each <= 1,000 UTF-8 bytes
```

Provider-specific structured-output features reduce malformed responses but do not replace local validation.

## 16. Failure semantics

Provider differences must not leak into the anti-fraud domain result.

All of the following normalize to unavailable assistance:

- invalid provider output;
- unsupported model at runtime;
- provider 4xx/5xx;
- timeout;
- caller cancellation;
- transport error;
- provider refusal/block;
- malformed JSON;
- missing expected output;
- oversized response.

Required invariant:

```text
successful deterministic rule result
+ any LLM provider failure
= successful deterministic rule result unchanged
+ assistance.status = unavailable
```

## 17. Provider conformance tests

After individual adapters exist, add shared conformance tests for semantics that can be provider-independent.

The suite should verify through factories/fakes that each registered provider:

- rejects blank credential;
- rejects unsafe model configuration;
- performs at most one provider request per `Assist` call;
- respects pre-cancelled context;
- never returns an authority-bearing field because the Go return type cannot represent one;
- rejects malformed supplemental output;
- does not leak injected secret text in returned errors;
- works through `llmassist.Service` timeout/fallback behavior.

Protocol-specific HTTP assertions remain in each adapter's own tests.

## 18. CI contract

GitHub Actions must require zero real provider credentials and perform zero real calls to OpenAI, Gemini or DeepSeek.

Every adapter uses an injected HTTP doer/transport in tests.

Full repository gates remain mandatory:

```text
Backend Test
Backend Race (including llmassist)
Backend Vet
PostgreSQL service
Redis service
Frontend Install
Frontend Typecheck
Frontend Build
Extension Test
```

## 19. Logging and secret safety

Do not log:

- provider API keys;
- Authorization or `x-goog-api-key` headers;
- raw provider request bodies;
- raw provider response bodies;
- analyzed raw text;
- prompts;
- hidden provider reasoning.

Generic operational metadata may be designed later, but raw secrets/content remain prohibited.

## 20. Existing HTTP compatibility

These existing routes remain deterministic-only:

```text
POST /api/v1/analysis/text
POST /api/v1/analysis/preview
```

Enabling or changing `LLM_ASSISTANCE_PROVIDER` must never silently alter either route.

A future cost-bearing assisted-analysis route requires a separate design covering explicit opt-in, authorization, Redis rate/cost limiting, request limits and user-facing privacy disclosure.

## 21. Implementation sequence

After this design merges, execute bounded slices in order.

### Slice A — Registry and generic model configuration

- implement `Registry` / `Factory` / `ProviderConfig`;
- replace `OPENAI_MODEL` with `LLM_ASSISTANCE_MODEL`;
- centralize selected-provider credential resolution;
- migrate existing OpenAI provider construction into Registry;
- remove hard-coded `gpt-5.6` startup requirement;
- keep all OpenAI protocol/security behavior unchanged;
- tests prove OpenAI can use a configured model string and unknown providers fail closed.

### Slice B — Gemini adapter

- fixed Gemini `generateContent` origin;
- model-path safety validation;
- `x-goog-api-key` auth;
- structured JSON schema;
- fake-HTTP success/failure/security tests.

### Slice C — DeepSeek adapter

- fixed `/chat/completions` endpoint;
- Bearer auth;
- JSON Output request;
- strict local schema decode;
- fake-HTTP success/failure/security tests.

### Slice D — Provider conformance suite

- common factory/config/error/secret/cancellation tests;
- keep provider protocol tests separate.

### Slice E — Explicit assisted-analysis transport design

Only after A-D are green may the project design the public/operator HTTP path that actually selects and invokes external LLM assistance.

## 22. Non-goals

This design does not add:

- assisted-analysis HTTP transport;
- frontend provider/model selector;
- browser-extension LLM wiring;
- LLM result persistence;
- provider billing UI;
- retries;
- streaming;
- arbitrary provider base URL;
- OpenAI-compatible custom provider;
- Ollama/vLLM/local-model provider;
- multiple simultaneously active provider profiles;
- rule mutation by LLM;
- tools/function calling/browsing/code execution.

## 23. Roadmap correction

Repository documentation must reflect current live implementation:

- the browser-extension prototype is already delivered;
- provider-neutral LLM core is already delivered;
- the OpenAI adapter is already delivered;
- current AI roadmap work is configurable multi-provider support followed by explicit opt-in assisted-analysis transport.

## 24. Acceptance summary

```text
Rule engine remains authority                         YES
Provider interface remains common                     YES
Provider/model runtime configurable                   YES
Provider-specific secrets                             YES
Fixed production origins for three adapters           YES
Generic arbitrary base URL                            NO
Existing analysis routes silently call LLM            NO
OpenAI adapter preserved                              YES
Gemini adapter planned                                YES
DeepSeek adapter planned                              YES
Provider failures break deterministic analysis        NO
CI real paid-provider calls                           NO
Assisted-analysis public route in this design         NO
```

## 25. External protocol references used for this design

The provider-specific protocol choices were checked against the current official documentation before freezing #74:

- Google Gemini API reference: `https://ai.google.dev/api`
- Google Gemini structured output: `https://ai.google.dev/gemini-api/docs/generate-content/structured-output`
- Google Gemini model catalog: `https://ai.google.dev/gemini-api/docs/models`
- DeepSeek Chat Completions: `https://api-docs.deepseek.com/api/create-chat-completion`
- DeepSeek JSON Output: `https://api-docs.deepseek.com/guides/json_mode/`
- DeepSeek model/change log: `https://api-docs.deepseek.com/updates/`

Provider documentation is time-sensitive. These references justify the protocol boundary, not a permanent hard-coded model list.
