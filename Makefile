SHELL := /usr/bin/env bash
COMPOSE := docker compose -f infra/docker-compose.yml

GOLANGCI_LINT_VERSION := v2.13.2
GOLANGCI_LINT := api/bin/golangci-lint

.PHONY: verify format-check lint typecheck generate generate-check test guards \
        verify-go test-go test-ts \
        dev-infra dev-infra-down migrate seed api bot

## verify: THE GATE — format, lint, typecheck, generated-code-fresh, tests, guards.
verify: format-check lint typecheck generate-check test guards

## format-check: Biome (TS) + gofmt (Go), check only, no writes.
format-check:
	pnpm exec biome ci .
	cd api && test -z "$$(gofmt -l .)"

## lint: golangci-lint (Go). Biome's lint pass already ran as part of
## `biome ci` in format-check, so it is not repeated here.
lint: verify-go

verify-go: $(GOLANGCI_LINT)
	cd api && bin/golangci-lint run ./...

## $(GOLANGCI_LINT): install the pinned golangci-lint binary into api/bin
## (gitignored) if it is not already there, via the project's own official
## install script fetched at the pinned tag (not @master, and not piped
## straight into sh).
$(GOLANGCI_LINT):
	@mkdir -p api/bin
	@tmpscript="$$(mktemp)"; \
	trap 'rm -f "$$tmpscript"' EXIT; \
	curl -sSfL -o "$$tmpscript" "https://raw.githubusercontent.com/golangci/golangci-lint/$(GOLANGCI_LINT_VERSION)/install.sh"; \
	sh "$$tmpscript" -b api/bin $(GOLANGCI_LINT_VERSION)

## typecheck: TypeScript project references across the workspace.
typecheck:
	pnpm -r --if-present typecheck

## generate: oapi-codegen + sqlc (Go) and openapi-typescript (TS client).
generate:
	cd api && go tool sqlc generate
	# oapi-codegen (contracts/openapi.yaml -> api/gen) is wired in T3.
	pnpm --filter @savdo/api-client --if-present generate

## generate-check: generated code must be committed and fresh.
generate-check: generate
	git diff --exit-code -- api/gen packages/api-client api/internal/db

## test: Go tests (unit + testcontainers) and TS tests (Vitest).
test: test-go test-ts

test-go:
	cd api && go test ./...

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

## migrate: run goose SQL migrations (via the savdo CLI) against DATABASE_URL.
migrate:
	@set -a; . infra/.env; set +a; \
	cd api && go run ./cmd/savdo migrate up

## seed: load development seed data.
seed:
	@if [ -d api/cmd/seed ]; then \
		cd api && go run ./cmd/seed; \
	else \
		echo "skip: cmd/seed not implemented yet (Phase 1)"; \
	fi

## api: run the API server.
api:
	@set -a; . infra/.env; set +a; \
	cd api && go run ./cmd/api

## bot: run the Telegram bot.
bot:
	@if [ -d api/cmd/bot ]; then \
		cd api && go run ./cmd/bot; \
	else \
		echo "skip: cmd/bot not implemented yet (Phase 7)"; \
	fi
