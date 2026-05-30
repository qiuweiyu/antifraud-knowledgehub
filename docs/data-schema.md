# Data Schema

Seed data lives in `data/` and is designed for community review.

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
