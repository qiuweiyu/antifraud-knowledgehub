#!/usr/bin/env sh
curl -X POST http://localhost:8080/api/v1/analysis/text \
  -H "Content-Type: application/json" \
  -d '{"text":"客服说账户异常，需要转账到安全账户"}'
