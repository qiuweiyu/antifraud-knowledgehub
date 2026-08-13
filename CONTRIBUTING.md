# Contributing

AntiFraud-KnowledgeHub welcomes contributions that improve public-interest anti-fraud education and developer integration.

## What to Contribute

- New scam categories.
- New explainable risk rules.
- Anonymous scam case samples.
- False-positive fixes.
- Documentation and translations.

## Data Safety

Do not submit real personal data, phone numbers, ID numbers, bank cards, private chat identifiers or unredacted screenshots. Case samples must be anonymized and should use placeholders such as `某用户`, `某平台` and `示例链接`.

## Development

```bash
make dev
make test
```

For a local backend-only run without Docker:

```bash
cd backend
DATABASE_DRIVER=sqlite DATABASE_DSN=afkh-dev.db go run ./cmd/server
```

Before opening a pull request, run:

```bash
cd backend
go test ./...
go vet ./...
cd ../frontend
npm run typecheck
npm run build
```

## Pull Request Checklist

- Keep changes scoped to one feature, fix, or documentation update.
- Include tests when changing backend behavior or API contracts.
- Include screenshots when changing visible frontend behavior.
- Do not include generated local databases, secrets, real chat logs, or real victim data.
- Link related issues using `Fixes #...` when the pull request fully resolves an issue.

## Rule and Case Contributions

Risk rules should be explainable, auditable, and tied to concrete public-safety signals. Include a short explanation, severity rationale, and one anonymized example when practical.

Contributors can validate a rule draft before submission with `POST /api/v1/rules/validate`.

Case contributions must be synthetic or fully anonymized. Prefer short, structured examples that demonstrate a tactic without preserving real-world identifiers.
