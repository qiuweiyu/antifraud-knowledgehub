# Analysis Preview API

The browser-extension prototype uses a dedicated no-write analysis endpoint so explicitly selected browser text is not silently added to the normal analysis-history table.

## Endpoint

```text
POST /api/v1/analysis/preview
Content-Type: application/json
```

Request:

```json
{
  "text": "客服说账户异常，需要转账到安全账户"
}
```

A valid request loads the same enabled `RiskRule` set and uses the same explainable risk-engine logic as `POST /api/v1/analysis/text`.

Success uses the standard response envelope:

```json
{
  "success": true,
  "data": {
    "risk_score": 45,
    "risk_level": "medium",
    "matched_rules": [
      {
        "rule_code": "example_rule",
        "rule_name": "Example rule",
        "category_code": "fake_customer_service",
        "weight": 45,
        "severity": "high",
        "evidence": "安全账户",
        "explanation": "...",
        "recommendation": "..."
      }
    ],
    "summary": "...",
    "recommendations": ["..."]
  }
}
```

Invalid JSON or a missing/empty `text` field returns:

```text
HTTP 400
error.code = invalid_analysis_request
```

## Persistence boundary

`/api/v1/analysis/preview` creates **zero** `AnalysisRecord` rows for both valid and invalid requests. It is a read-only risk preview over the enabled rules.

The existing endpoint remains intentionally different:

```text
POST /api/v1/analysis/text
```

It continues to persist the normal `AnalysisRecord`, including the submitted input text, for dashboard/history behavior. The preview endpoint does not change or weaken that existing contract.

The no-write statement is limited to analysis-history/workflow persistence performed by this endpoint. It is not a claim of cryptographic privacy, authenticated localhost, or protection from a compromised local machine.

## Browser-extension use

The first extension slice calls only:

```text
http://127.0.0.1:8080/api/v1/analysis/preview
```

The extension sends only the explicitly selected text in `{ "text": ... }`, with credentials omitted. It does not send page URL, title, browser history, cookies, or authentication data.

See:

- `docs/browser-extension-prototype-design.md`
- `browser-extension/README.md`
