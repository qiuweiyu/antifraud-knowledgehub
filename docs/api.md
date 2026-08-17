# API

Base path: `/api/v1`

## Health

`GET /health`

```json
{"status":"ok","service":"antifraud-knowledgehub"}
```

## Categories

- `GET /categories`
- `POST /categories`
- `GET /categories/{id}`
- `PUT /categories/{id}`
- `DELETE /categories/{id}`

## Rules

- `GET /rules?q=&category_code=&severity=`
- `POST /rules/validate`
- `POST /rules`
- `GET /rules/{id}`
- `PUT /rules/{id}`
- `PATCH /rules/{id}/toggle`
- `DELETE /rules/{id}`

### Validate Rule Draft

`POST /api/v1/rules/validate`

Validates a rule draft before it is saved. This endpoint does not create a
`RiskRule` record and has no database write side effects.

Request:

```json
{
  "code": "community_safe_channel",
  "name": "Official channel check",
  "category_code": "fake_customer_service",
  "rule_type": "keyword",
  "pattern": "official channel",
  "weight": 20,
  "severity": "medium"
}
```

Response:

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

## Controlled Rule Submissions

`POST /api/v1/rule-submissions`

This endpoint is a controlled maintainer transport, not anonymous public community access. It is registered only when `RULE_SUBMISSIONS_ENABLED=true` and the submission transport configuration is valid.

Requirements:

- `Authorization: Bearer <RULE_SUBMISSION_WRITE_TOKEN>`
- `Content-Type: application/json`
- request body no larger than 32 KiB
- JSON must contain only fields supported by the existing rule `DraftRequest`
- Redis-backed per-credential and global rate limits must be available

Persistence delegates to the replay-safe pending-submission service. A new valid canonical draft creates one non-executable `RuleSubmission` with server-assigned status `pending`; an exact retry returns the already existing pending submission instead of creating another row. Neither create nor replay creates or modifies a `RiskRule`.

Exact replay is based on a server-computed SHA-256 fingerprint of the normalized persisted draft snapshot. The digest is internal: clients do not send it and it is not exposed in the response. Same `code` with different persisted content remains a distinct proposal. Authorization and Redis rate limiting run before replay detection, so retries still consume normal abuse-control budget.

Example request:

```json
{
  "code": "community_safe_account_request",
  "name": "Safe account transfer request",
  "category_code": "fake_customer_service",
  "rule_type": "keyword",
  "pattern": "安全账户",
  "weight": 40,
  "severity": "high",
  "explanation": "Synthetic anti-fraud example.",
  "recommendation": "Verify the request independently before transferring funds."
}
```

Important response statuses:

- `201` — one new pending submission created
- `200` — exact replay; the existing pending submission is returned with the same ID
- `400` — malformed JSON, unknown fields, trailing JSON, or invalid rule draft for a new digest
- `401` — missing or invalid controlled write credential
- `413` — request body exceeds 32 KiB
- `415` — unsupported content type
- `429` — submission rate limit exceeded
- `503` — Redis/rate-limiter protection cannot be evaluated

The route is intentionally unavailable when the feature flag is disabled. Credentials, raw submission bodies, canonical JSON, and draft digests must not be logged.

## Cases

- `GET /cases?q=&category_code=`
- `POST /cases`
- `GET /cases/{id}`
- `PUT /cases/{id}`
- `DELETE /cases/{id}`

## Text Analysis

`POST /analysis/text`

```json
{"text":"客服说账户异常，需要转账到安全账户"}
```

The response includes `risk_score`, `risk_level`, `matched_rules`, `summary` and `recommendations`.

## Analysis Stats

`GET /analysis/stats`

Returns aggregate counts for categories, rules, enabled rules, anonymous cases, analysis records, risk-level buckets and category coverage. The dashboard overview page uses this endpoint instead of loading every rule and case just to calculate counts.

## Response Envelope

Successful responses use:

```json
{"success":true,"data":{}}
```

Errors use:

```json
{"success":false,"error":{"code":"invalid_request","message":"human readable message"}}
```
