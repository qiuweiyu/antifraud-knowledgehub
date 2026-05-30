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

- `GET /rules`
- `POST /rules`
- `GET /rules/{id}`
- `PUT /rules/{id}`
- `PATCH /rules/{id}/toggle`
- `DELETE /rules/{id}`

## Cases

- `GET /cases`
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

## Response Envelope

Successful responses use:

```json
{"success":true,"data":{}}
```

Errors use:

```json
{"success":false,"error":{"code":"invalid_request","message":"human readable message"}}
```
