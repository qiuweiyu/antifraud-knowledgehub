# Roadmap

## v0.1 MVP

- Explainable rule engine.
- Category, rule and anonymous case APIs.
- Vue3 dashboard and text analysis page.
- Docker Compose local startup.

## v0.1.x Community Workflow Foundation

Current `main` now includes:

- Rule contribution format and maintainer review checklist.
- Rule draft validation.
- Default-off controlled pending submission transport.
- Replay-safe submission idempotency.
- Human approve/reject review with audit event.
- Independent default-off review authorization/transport.
- Approved-only publication with publication provenance event.
- Independent default-off publication authorization/transport.
- PostgreSQL concurrency and rollback acceptance for critical review/publication paths.

These transports are controlled maintainer endpoints, not anonymous public community-write access.

## v0.2 Community Rules

Still planned beyond the delivered foundation:

- Rule versioning/history and per-rule changelog.
- Broader contributor identity/permission design if public mutation access is later required.
- Maintainer/community UX beyond the current controlled API transports.
- Additional rule testing/review utilities driven by real contribution needs.

## v0.3 Documentation and Examples

- Expanded API usage examples.
- Developer quickstart improvements.
- Example datasets and use-case documentation.

## v0.4 AI-assisted Analysis Adapter

- Optional provider interface.
- Mock provider remains the default.
- No paid external API is required for local development.
- Human review and explainability remain authoritative.

## v0.5 Browser Extension

- Browser-side text submission.
- Local API integration.
- Explainable warning result.

## Later

- Multi-language data and locale-aware dashboard labels.
