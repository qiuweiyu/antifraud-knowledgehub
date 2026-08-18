# Roadmap

AntiFraud-KnowledgeHub is an early-stage open-source project focused on building an explainable anti-fraud knowledge base and risk analysis toolkit for Chinese-speaking online scenarios.

## v0.1 MVP

- Explainable rule engine
- Structured scam categories
- Anonymous case samples
- Text risk analysis API
- Vue3 dashboard
- CLI tool
- Docker Compose local setup

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

- Rule versioning/history
- Per-rule changelog/history presentation
- Broader contributor identity/permission model only if public mutation access is intentionally introduced
- Maintainer/community UX beyond the current controlled API transports
- Additional rule testing/review utilities as real workflow needs emerge

The delivered v0.1.x workflow foundation should not be described as completion of all v0.2 work.

## v0.3 Documentation and Examples

- Real dashboard screenshots
- API usage examples
- Developer quickstart
- Example datasets
- Use-case documentation

## v0.4 AI-assisted Adapter

- Optional LLM provider interface
- Human-review workflow
- Explainability guardrails
- Prompt and output safety guidelines

## v0.5 Browser Extension Prototype

- Text selection risk check
- Browser popup UI
- Local API integration
- Explainable warning result

## Long-term Vision

The project aims to become a community-maintained anti-fraud knowledge hub that helps developers, educators, researchers, and public-interest teams build transparent fraud detection and education tools.
