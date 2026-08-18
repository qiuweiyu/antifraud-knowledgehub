# Rule Contribution Format

This document defines the current contribution contract for new explainable anti-fraud rules in AntiFraud-KnowledgeHub.

The goal is to make rule contributions consistent, auditable, safe to publish, and easy for maintainers to review. The same `DraftRequest` contract is used by validation and the controlled submission workflow.

> Scope: this format targets **new rule drafts**. The repository now also implements a separate default-off controlled lifecycle for pending submission, human review and publication. That lifecycle does not turn this project into an anonymous public write service or a contributor account/RBAC system. See [Community Rule Workflow](community-rule-workflow.md).

## Where rules and proposals live today

The seed rule set is stored in:

`data/risk_rules.zh-CN.json`

Executable rules are persisted as `RiskRule` records. Unreviewed controlled proposals are persisted separately as `RuleSubmission` records and do **not** enter the risk engine.

There are therefore two supported contribution paths:

1. normal open-source pull requests using this format and synthetic/anonymized examples, and
2. a deployment-controlled, default-off HTTP workflow for maintainers that separates submission, terminal review and publication credentials.

External contributors do not need access to the controlled bearer credentials in order to contribute through GitHub.

## Required fields

| Field | Requirement | Notes |
| --- | --- | --- |
| `code` | required | Unique rule identifier. Trimmed, non-empty, max 120 characters. |
| `name` | required | Human-readable rule name. Trimmed, non-empty, max 180 characters. |
| `category_code` | required | Must reference an existing category. Max 100 characters. |
| `rule_type` | required | Must be one of the currently supported rule types below. |
| `pattern` | required | Match expression used by the selected rule type. |
| `severity` | required | Must be one of the currently supported severity values below. |

## Optional fields

| Field | Requirement | Notes |
| --- | --- | --- |
| `description` | optional | Short description of the suspicious signal. |
| `weight` | optional in JSON shape, validated as an integer from 0 to 100 | Use the smallest weight that reasonably represents the signal. |
| `enabled` | optional | Defaults to enabled when created through the direct rule API if omitted; controlled publication preserves the approved stored value, including explicit `false`. |
| `explanation` | optional but strongly recommended | Explain why the signal is suspicious in plain language. |
| `recommendation` | optional but strongly recommended | Give a safe, practical action a user can take. |

## Supported rule types

The current validator accepts exactly:

- `keyword` — matches one or more explicit terms or phrases.
- `pattern` — represents an existing multi-signal pattern used by the current rule engine.
- `regex` — uses a regular expression; the expression must compile successfully.
- `semantic_placeholder` — a placeholder type already recognized by the current engine. It is not an instruction to add an external AI provider.

Do not invent a new rule type in a contribution without a separate code change, tests, and review.

## Supported severity values

The current validator accepts exactly:

- `low`
- `medium`
- `high`
- `critical`

Choose severity based on the likely harm and confidence of the signal. Severity is not a substitute for evidence: a strong label needs a clear explanation and proportionate rule behavior.

## Weight

`weight` must be between `0` and `100` inclusive.

Use weight conservatively:

- A broad or weak signal should generally have a lower weight.
- A specific, high-confidence scam signal can justify a higher weight.
- Avoid giving a single generic phrase enough weight to dominate the whole risk result without a strong reason.

Maintainers may ask for weight changes when false-positive risk is too high.

## Safe example

The following is a synthetic example for documentation and testing:

```json
{
  "code": "request_remote_screen_share",
  "name": "诱导开启屏幕共享",
  "description": "对方要求用户开启屏幕共享以处理所谓账户问题。",
  "category_code": "fake_customer_service",
  "rule_type": "keyword",
  "pattern": "开启屏幕共享,共享屏幕处理账户",
  "weight": 24,
  "severity": "high",
  "enabled": true,
  "explanation": "诈骗者可能利用屏幕共享获取验证码、支付信息或其他敏感内容。",
  "recommendation": "不要向陌生人共享屏幕，并通过官方渠道独立核实对方身份。"
}
```

The example must stay synthetic. Do not replace placeholders with a real victim name, phone number, bank card, private chat identifier, access token, credential, or unredacted screenshot.

## Examples that should be rejected or revised

### Missing explanation

A rule that only contains a pattern and a high score is difficult to audit. Add a clear explanation of why the signal matters.

### Overly broad matching

Avoid a generic pattern such as `转账` with a critical severity and very high weight unless additional context makes the signal sufficiently specific. Broad rules create false positives.

### Real personal data

Do not submit rules or examples containing real phone numbers, bank cards, ID numbers, private account identifiers, victim names, credentials, tokens, or copied private chat logs.

### Invalid regular expression

For `regex`, the pattern must compile. The validator rejects malformed expressions such as an unmatched `[`.

## Validate before review

There are two supported validation paths.

### Dashboard

Open the **Rules** page and choose **验证规则草稿 / Validate Draft**. Fill in the draft fields and run validation. The UI displays structured errors and warnings and does not save the rule.

### API

Send the draft to:

`POST /api/v1/rules/validate`

Example:

```bash
curl -X POST http://localhost:8080/api/v1/rules/validate \
  -H "Content-Type: application/json" \
  -d '{
    "code":"request_remote_screen_share",
    "name":"诱导开启屏幕共享",
    "category_code":"fake_customer_service",
    "rule_type":"keyword",
    "pattern":"开启屏幕共享,共享屏幕处理账户",
    "weight":24,
    "severity":"high",
    "explanation":"诈骗者可能利用屏幕共享获取敏感信息。",
    "recommendation":"停止共享并通过官方渠道核实。"
  }'
```

A valid response contains:

```json
{
  "success": true,
  "data": {
    "valid": true,
    "errors": [],
    "warnings": []
  }
}
```

Validation checks structural and repository-backed constraints such as required fields, duplicate codes, category existence, supported types/severities, weight bounds, and regex compilation.

**Validation is not approval.** A draft that is structurally valid can still be rejected during human review for weak evidence, excessive false-positive risk, unsafe data, unclear wording, or poor recommendations.

> Note: the current validator is optimized for new rule codes. Sending the code of an already-persisted rule can produce `duplicate_code`; modifications to existing rules still require normal tests and human review.

## Human review checklist

Before approving a new rule contribution, a maintainer should confirm:

- [ ] The draft passes the current rule validator.
- [ ] `code` is stable, descriptive, and unique.
- [ ] `category_code` matches an existing and appropriate scam category.
- [ ] `rule_type` matches how the current engine actually evaluates the pattern.
- [ ] The pattern is specific enough to avoid obvious false positives.
- [ ] Regex rules are readable, bounded in purpose, and compile successfully.
- [ ] `weight` is proportionate to confidence and expected harm.
- [ ] `severity` has a clear rationale.
- [ ] `explanation` states why the signal is suspicious and remains understandable to non-experts.
- [ ] `recommendation` gives a safe action without encouraging risky confrontation or data disclosure.
- [ ] All examples are synthetic or fully anonymized.
- [ ] No secrets, credentials, real victim data, bank-card data, ID numbers, phone numbers, or private chat identifiers are included.
- [ ] Tests or safe examples cover the intended match behavior when practical.
- [ ] The contribution does not silently introduce a new rule type or external AI dependency.

## Controlled submission, review and publication

Deployments may enable the controlled workflow described in [Community Rule Workflow](community-rule-workflow.md).

The lifecycle is intentionally split:

```text
Draft validation
  -> pending RuleSubmission
  -> approved/rejected terminal review + review event
  -> approved-only publication
  -> RiskRule + publication event
```

Important boundaries:

- pending submissions never execute as `RiskRule` records,
- approval does not publish a rule,
- write, review and publication bearer credentials are independent,
- review/publication actors are server-owned operational labels,
- publication copies the approved stored snapshot rather than accepting rule fields from the publication request,
- failed controlled review/publication paths are covered by zero-write security tests,
- AI is not a final approval authority.

The exact HTTP contracts and status codes are documented in [API](api.md).

## Pull request expectations

For a new seed rule contribution:

1. Update `data/risk_rules.zh-CN.json` using the format above.
2. Validate the draft through the UI or API.
3. Add or update tests when the rule behavior requires new engine coverage.
4. Run the repository checks described in `CONTRIBUTING.md`.
5. In the pull request, explain the scam signal, expected matches, false-positive considerations, severity rationale, and any test evidence.

For a contribution intended to enter through the controlled maintainer transport, external contributors should still provide the same evidence and safety context through the project's normal collaboration channel; repository bearer credentials should not be distributed merely to bypass the pull-request workflow.

Keep each pull request scoped. A rule contribution should not also introduce authentication, reviewer roles, AI providers, unrelated UI changes, or large dependency upgrades unless those changes are the explicit purpose of a separate issue and review.
