SHELL := /usr/bin/env bash
COMPOSE := docker compose -f infra/docker-compose.yml

.PHONY: verify format-check lint typecheck generate generate-check test guards \
        verify-go test-go test-ts \
        dev-infra dev-infra-down migrate seed api bot

## verify: THE GATE — format, lint, typecheck, generated-code-fresh, tests, guards.
verify: format-check lint typecheck generate-check test guards

## format-check: Biome (TS) + gofmt (Go), check only, no writes.
format-check:
	pnpm exec biome ci .
	@if [ -f api/go.mod ]; then \
		unformatted="$$(gofmt -l ./api)"; \
		if [ -n "$$unformatted" ]; then \
			echo "gofmt: files not formatted:"; echo "$$unformatted"; exit 1; \
		fi; \
	else \
		echo "skip: api/ not scaffolded yet (Phase 0 T2)"; \
	fi

## lint: golangci-lint (Go). Biome's lint pass already ran as part of
## `biome ci` in format-check, so it is not repeated here.
lint: verify-go

verify-go:
	@if [ -f api/go.mod ]; then \
		cd api && go tool golangci-lint run ./...; \
	else \
		echo "skip: api/ not scaffolded yet (Phase 0 T2)"; \
	fi

## typecheck: TypeScript project references across the workspace.
typecheck:
	pnpm -r --if-present typecheck

## generate: oapi-codegen + sqlc (Go) and openapi-typescript (TS client).
generate:
	@if [ -f api/go.mod ]; then \
		cd api && go generate ./...; \
	else \
		echo "skip: api/ not scaffolded yet (Phase 0 T2)"; \
	fi
	pnpm --filter @savdo/api-client --if-present generate

## generate-check: generated code must be committed and fresh.
generate-check: generate
	git diff --exit-code -- api/gen packages/api-client api/internal/db

## test: Go tests (unit + testcontainers) and TS tests (Vitest).
test: test-go test-ts

test-go:
	@if [ -f api/go.mod ]; then \
		cd api && go test ./...; \
	else \
		echo "skip: api/ not scaffolded yet (Phase 0 T2)"; \
	fi

test-ts:
	pnpm -r --if-present test

## guards: repo-wide grep guard rails (money types, stock writes, secrets, ...).
guards:
	bash scripts/guards.sh

## dev-infra: bring up local Postgres.
dev-infra:
	@if [ ! -f infra/.env ]; then cp infra/.env.example infra/.env; fi
	$(COMPOSE) up -d
	$(COMPOSE) ps

## dev-infra-down: stop local infra (keeps the data volume).
dev-infra-down:
	$(COMPOSE) down

## migrate: run goose SQL migrations against DATABASE_URL.
migrate:
	@if [ -f api/go.mod ]; then \
		cd api && go tool goose up; \
	else \
		echo "skip: api/ not scaffolded yet (Phase 0 T2)"; \
	fi

## seed: load development seed data.
seed:
	@if [ -f api/go.mod ]; then \
		cd api && go run ./cmd/seed; \
	else \
		echo "skip: api/ not scaffolded yet (Phase 0 T2)"; \
	fi

## api: run the API server.
api:
	@if [ -f api/go.mod ]; then \
		cd api && go run ./cmd/api; \
	else \
		echo "skip: api/ not scaffolded yet (Phase 0 T2)"; \
	fi

## bot: run the Telegram bot.
bot:
	@if [ -f api/go.mod ]; then \
		cd api && go run ./cmd/bot; \
	else \
		echo "skip: api/ not scaffolded yet (Phase 0 T2)"; \
	fi
