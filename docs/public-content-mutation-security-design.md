# Public Content Mutation Security Design

Status: **Frozen for P0 remediation implementation**

Issue: #105

Baseline audited: `e3cff12a2b5954cf38066491b71a4da011231574`

## 1. Problem statement

The default `/api/v1` router currently registers Category, Risk Rule, and Scam Case CRUD handlers without an authentication or authorization boundary. In particular, anonymous callers can currently reach direct `RiskRule` create/update/toggle/delete handlers. `RiskRule` is executable deterministic risk-engine input, so these routes allow an unauthenticated caller to alter the system's authoritative score/evidence behavior without passing the controlled submission -> review -> publication workflow.

The same root cause also exposes anonymous Category and Scam Case mutation. Those objects are not the score authority, but they are trusted public knowledge content and must not remain anonymously writable.

This is a P0 security issue. New Rule Versioning/History work is blocked until the direct mutation surface is removed; a version history cannot be authoritative while writers can bypass it.

## 2. Security invariant

The default public API is **read-only for persisted Category, Risk Rule, and Scam Case content**.

The only public Rule POST retained by this remediation is `POST /api/v1/rules/validate`, whose contract is no persistence side effects.

A persistence mutation is exposed only through an explicitly designed, independently authorized, default-off transport. Today, that applies only to the existing Rule Submission / Review / Publication workflow.

CORS is never an authentication or authorization boundary.

## 3. Frozen public route matrix

### 3.1 Routes that remain publicly registered

| Method | Route | Persistence effect |
|---|---|---|
| GET | `/api/v1/categories` | none |
| GET | `/api/v1/categories/:id` | none |
| GET | `/api/v1/rules` | none |
| GET | `/api/v1/rules/:id` | none |
| POST | `/api/v1/rules/validate` | none |
| GET | `/api/v1/cases` | none |
| GET | `/api/v1/cases/:id` | none |

Existing analysis, health, browser-session, assisted-analysis, and Swagger routes are outside this change except for regression verification that they are not accidentally modified.

### 3.2 Direct mutation routes that must no longer be registered

| Method | Route |
|---|---|
| POST | `/api/v1/categories` |
| PUT | `/api/v1/categories/:id` |
| DELETE | `/api/v1/categories/:id` |
| POST | `/api/v1/rules` |
| PUT | `/api/v1/rules/:id` |
| PATCH | `/api/v1/rules/:id/toggle` |
| DELETE | `/api/v1/rules/:id` |
| POST | `/api/v1/cases` |
| PUT | `/api/v1/cases/:id` |
| DELETE | `/api/v1/cases/:id` |

These routes are removed by **absence from route registration**, not by a permissive handler that performs its own optional check.

Expected default-router behavior for those exact method/path pairs is `404 Not Found` under Gin's current no-route behavior. The acceptance contract is that no matching handler executes and no persistence state changes; clients must not depend on a particular JSON error body.

## 4. Controlled Rule workflow remains the only HTTP write path

This remediation does not change the existing default-off controlled routes:

- `POST /api/v1/rule-submissions`
- `POST /api/v1/rule-submissions/:id/reviews`
- `POST /api/v1/rule-submissions/:id/publications`

Their existing independent credentials, feature flags, validation, replay/concurrency semantics, audit events, and negative security tests remain authoritative.

No direct CRUD route may reuse `RULE_SUBMISSION_WRITE_TOKEN`, `RULE_SUBMISSION_REVIEW_TOKEN`, or `RULE_SUBMISSION_PUBLICATION_TOKEN`. Those credentials are purpose-specific and must not be generalized into administrator credentials.

## 5. Category and Scam Case writes

There is no approved public Category or Scam Case mutation transport in this slice.

Maintainer updates may still occur through reviewed repository seed-data changes and existing seed/import mechanisms, or through a future separately designed administrative/contribution capability. This P0 fix does not invent an ad-hoc admin token or RBAC model.

## 6. Handler/code boundary

Implementation should make the public boundary obvious in code:

1. Category registration exposes only list/get.
2. Rule registration exposes only list/get/validate.
3. Case registration exposes only list/get.
4. Direct create/update/toggle/delete handlers should be deleted when they have no remaining internal callers, rather than retained as dormant HTTP mutation code.
5. Seed/import and controlled publication services are not routed through those deleted handlers and remain separate persistence mechanisms.

This design does not require renaming the public `Register` functions if a smaller diff can make their read-only nature unambiguous, but tests must inspect the resulting router surface rather than trust naming alone.

## 7. Frontend compatibility

Current Vue API clients use:

- Category: list only.
- Rule: list and no-write validation only.
- Scam Case: list only.

Therefore the P0 route removal requires no frontend behavior change. Frontend CI remains mandatory to detect accidental coupling.

## 8. Documentation contract

The implementation PR must update public API documentation so it no longer advertises the removed direct mutation routes.

Documentation must explicitly state:

- Category/Rule/Case persisted content is read-only through the default public API.
- `POST /rules/validate` does not persist.
- Controlled Rule Submission / Review / Publication is the only current HTTP mutation workflow for rules.
- There is no anonymous Category or Scam Case write API.

Any README or architecture statement that implies anonymous direct mutation must be corrected if found during the bounded implementation diff.

## 9. Regression and acceptance tests

The implementation must add router-level tests against the same router construction used by production startup.

### 9.1 Required negative route matrix

For each removed direct mutation route:

1. Seed a known persistence baseline.
2. Send a syntactically plausible mutation request.
3. Assert no matching route succeeds; expected status is `404` under the current Gin router configuration.
4. Re-read persistence state and assert zero Category/Rule/Case mutation.

At minimum the matrix must cover all ten method/path pairs in section 3.2.

### 9.2 Required positive compatibility

Verify that:

- public Category/Rule/Case GET routes remain registered;
- `POST /rules/validate` remains registered and has zero write side effects;
- controlled Rule Submission / Review / Publication route registration remains feature-gated and independently authorized;
- existing analysis behavior continues to read persisted rules normally.

### 9.3 Route-surface guard

Add a focused test that inspects Gin routes or exercises the exact method/path matrix so a future contributor cannot accidentally re-register an anonymous direct mutation without failing CI.

The test should fail on route presence itself, even if the handler happens to reject one particular payload.

## 10. Database and migration impact

No schema migration is required.

Do not change `RiskRule`, Category, Scam Case, submission/review/publication models, AutoMigrate order, indexes, or existing audit-event schema in this P0 slice.

## 11. Compatibility and release policy

Removing previously documented anonymous write routes is intentionally security-breaking. There is no deprecation period for an authentication-bypass surface.

`v0.1.3` remains an immutable historical release. After this P0 fix is merged and full CI is green, release readiness should consider a patch release (expected `v0.1.4`) that explicitly calls out the security hardening.

Do not retag or rewrite `v0.1.3`.

## 12. Out of scope

This P0 remediation does **not** add:

- general user accounts;
- administrator sessions;
- OAuth;
- RBAC;
- public contributor identity;
- a Category contribution workflow;
- a Scam Case contribution workflow;
- Rule Versioning/History;
- frontend maintainer editing UI.

Any future persisted mutation transport must be separately Design First and must include authentication, authorization, auditability, bounded inputs, concurrency semantics, and negative security tests.

## 13. Implementation sequence

After this design PR is merged:

1. Re-read live `main` and verify no relevant drift.
2. Create one bounded implementation Issue/branch tied to #105.
3. Remove direct mutation registrations and unused handlers.
4. Add router-level negative route-surface tests and persistence-no-change assertions.
5. Update `docs/api.md` and any directly contradictory public text.
6. Run actual GitHub Actions.
7. Review exact diff and re-check live-main drift.
8. Merge only with all required CI gates green.
9. Re-verify live router source and close #105 only after implementation evidence is on `main`.
10. Resume Rule Versioning/History Design First only after the P0 is closed.