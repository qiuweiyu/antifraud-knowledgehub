# Data Schema

This document covers the repository's seed JSON shapes and summarizes the runtime persistence concepts that matter for rule contributions.

Seed data lives in `data/` and is designed for community review. Runtime PostgreSQL models include additional workflow/audit fields that are intentionally not represented as contributor-editable seed JSON.

## Category JSON

```json
{
  "code": "fake_customer_service",
  "name": "冒充客服",
  "description": "冒充平台或客服制造风险事件。",
  "severity_default": "high"
}
```

## Risk Rule JSON

```json
{
  "code": "safe_account_transfer",
  "name": "安全账户转账",
  "category_code": "fake_customer_service",
  "rule_type": "keyword",
  "pattern": "安全账户,转账验证",
  "weight": 30,
  "severity": "critical",
  "enabled": true,
  "explanation": "要求转账到所谓安全账户是典型诈骗话术。",
  "recommendation": "不要向陌生账户转账，通过官方渠道核实。"
}
```

The same rule-draft fields are used by validation and controlled pending submissions. Server-owned workflow metadata such as submission status, review/publication event IDs, actor attribution, digests and source provenance are not client-editable rule fields.

## Scam Case JSON

```json
{
  "title": "匿名冒充客服案例",
  "category_code": "fake_customer_service",
  "content": "某用户收到自称客服的消息...",
  "summary": "冒充客服制造账户异常。",
  "risk_points": ["账户异常", "安全账户"],
  "tags": ["客服", "转账"],
  "source_type": "sample",
  "anonymized": true
}
```

## Runtime Rule Workflow Models

The current controlled workflow keeps proposals and executable rules separate:

- `RuleSubmission` — stored rule-draft snapshot with server-owned pending/terminal review status; pending rows are non-executable.
- `RuleSubmissionReviewEvent` — one terminal approve/reject audit event for a reviewed submission.
- `RuleSubmissionPublicationEvent` — provenance for the first publication of an approved submission, including review/rule identifiers, frozen rule code, actor attribution and source digest.
- `RiskRule` — executable rule used by the risk engine; publication-created rules carry nullable server-owned source-submission provenance.

Review approval does not create a `RiskRule`. Approved-only publication creates the initial `RiskRule` and publication event atomically. Later rule lifecycle changes do not rewrite the historical publication event.

See [Community Rule Workflow](community-rule-workflow.md) and [API](api.md) for the lifecycle and transport contracts.

## Contribution Rules

- Use stable `code` values for categories and rules.
- Keep examples anonymous.
- Prefer specific evidence patterns over broad keywords.
- Include explanations and recommendations for every rule.
- Add a sample that demonstrates the intended match when possible.
- Do not add server-owned workflow/audit fields to contributor rule JSON.
