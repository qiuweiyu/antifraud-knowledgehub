# Roadmap

Provider protocol planning for the current multi-provider AI slice was re-verified against official provider documentation on 2026-08-19.

AntiFraud-KnowledgeHub is an early-stage open-source project focused on building an explainable anti-fraud knowledge base and risk analysis toolkit for Chinese-speaking online scenarios.

## v0.1 MVP

Delivered:

- [x] Explainable rule engine
- [x] Structured scam categories
- [x] Anonymous case samples
- [x] Text risk analysis API
- [x] Vue3 dashboard
- [x] CLI tool
- [x] Docker Compose local setup

## v0.1.x Community Workflow Foundation

Delivered on current `main`:

- [x] Structured rule contribution format and human review checklist
- [x] Rule draft validation without persistence side effects
- [x] Default-off controlled pending submission transport
- [x] Replay-safe pending-submission idempotency
- [x] Human approve/reject terminal review with audit event
- [x] Default-off independent review authorization
- [x] Approved-only publication with provenance audit
- [x] Default-off independent publication authorization and HTTP transport
- [x] PostgreSQL-backed review/publication concurrency acceptance
- [x] Negative security tests for prohibited write paths

The controlled HTTP transports are maintainer-facing and do not represent anonymous public mutation access or a general contributor account/RBAC system.

## v0.2 Community Rules

Remaining broader community-rule work includes:

- [ ] Rule versioning/history
- [ ] Per-rule changelog/history presentation
- [ ] Broader contributor identity/permission model only if public mutation access is intentionally introduced
- [ ] Maintainer/community UX beyond the current controlled API transports
- [ ] Additional rule testing/review utilities as real workflow needs emerge

The delivered v0.1.x workflow foundation should not be described as completion of all v0.2 work.

## v0.3 Documentation and Examples

Partially delivered:

- [x] Real dashboard screenshots
- [x] API usage documentation
- [x] Developer quickstart
- [x] Example datasets
- [ ] Broader use-case documentation and tutorials

## v0.4 AI-assisted Adapter

Delivered foundation:

- [x] Provider-neutral LLM `Provider` interface
- [x] Provider-neutral assistance service
- [x] Deterministic rule-result authority boundary
- [x] Timeout/cancellation/fallback semantics
- [x] Deep-copy isolation against provider mutation of rule results
- [x] Bounded supplemental output validation
- [x] Prompt-injection and no-tool safety boundary
- [x] Disabled-by-default configuration foundation
- [x] First bounded OpenAI Responses adapter
- [x] Zero-real-provider-call fake-HTTP CI coverage for OpenAI

Current multi-provider work:

- [ ] Provider Registry / Factory
- [ ] Generic runtime `LLM_ASSISTANCE_MODEL` configuration
- [ ] Migrate OpenAI adapter to Registry and configurable model
- [ ] Gemini adapter
- [ ] DeepSeek adapter
- [ ] Shared provider conformance tests
- [ ] Explicit opt-in assisted-analysis HTTP transport with independent authorization/rate/cost controls
- [ ] Frontend provider/model selection only after the transport is frozen

The LLM remains supplemental. It must not replace rule evidence, risk scoring, human review or official-channel verification.

## v0.5 Browser Extension Prototype

Delivered on current `main`:

- [x] Chromium Manifest V3 prototype
- [x] Explicit context-menu selected-text analysis
- [x] Browser popup UI
- [x] Fixed loopback local API integration
- [x] Explainable score/rule/evidence/recommendation display
- [x] Session-only extension storage
- [x] No content scripts / no `<all_urls>`
- [x] No-write `/api/v1/analysis/preview` backend path
- [x] Extension Node CI tests

Future browser work, if product need justifies it:

- [ ] Packaging/store-distribution design
- [ ] Cross-browser support
- [ ] Explicit opt-in LLM-assisted browser flow only after the server-side assisted-analysis transport exists

## Long-term Vision

The project aims to become a community-maintained anti-fraud knowledge hub that helps developers, educators, researchers, and public-interest teams build transparent fraud detection and education tools.
