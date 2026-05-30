.PHONY: dev up down seed test lint backend-test frontend-build

dev:
	docker compose up --build

up:
	docker compose up -d --build

down:
	docker compose down

seed:
	cd backend && go run ./cmd/server --seed-only

test: backend-test frontend-build

lint:
	cd backend && go vet ./...
	cd frontend && npm run typecheck

backend-test:
	cd backend && go test ./...

frontend-build:
	cd frontend && npm run typecheck && npm run build
