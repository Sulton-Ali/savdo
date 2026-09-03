# 02 — Tech stack

Pinned versions and why. **Versions verified 2026-09-03** against official release pages
by the scribe (sources in the notes column). Agents never bump a pin from memory; a bump
is an owner decision, checked against the registry, in its own `chore(deps)` commit.

Where a template (e.g. `create-expo-app`, `create-tsrouter-app`) pins a different minor
than listed here, Phase 0 uses the template's pin and updates this table in the same
branch — with the reason.

## Go API and bot

| Item                  | Pin      | Why / notes                                                                                                  |
| --------------------- | -------- | ------------------------------------------------------------------------------------------------------------ |
| Go                    | 1.27.x   | Latest stable 1.27.1 (2026-09-01); dev machine has the CachyOS 1.27.0 build, fine. `go tool` directive needs ≥ 1.24 |
| Router                | stdlib   | `net/http` `ServeMux` with method + path patterns. No framework (D-11)                                        |
| pgx                   | v5.10.0  | Postgres driver; `pgxpool`; `pgtype.Numeric` for money                                                        |
| sqlc                  | v1.31.1  | Typed Go from SQL; `sqlc diff` in the gate. Pinned via `go tool` in `go.mod` (D-24)                             |
| goose                 | v3.28.0  | SQL migrations, embedded via `embed.FS`, run by `savdo migrate`; CLI via `go tool` (D-24)                      |
| oapi-codegen          | v2.8.0   | OpenAPI 3.1 → Go server interface (`std-http` generator) + types; runtime ≥ v1.6.0; via `go tool` (D-24)       |
| golangci-lint         | v2.13.2  | Lint gate; binary pinned and installed by the Makefile into `api/bin/`; config `api/.golangci.yml`            |
| testcontainers-go     | v0.44.0  | Real Postgres in integration tests (`modules/postgres`)                                                         |
| go-telegram/bot       | v1.25.0  | Bot API 10.3; modern, maintained. **Not** `go-telegram-bot-api` (older design)                                |
| anthropic-sdk-go      | v1.69.0  | First `internal/ai` provider (Claude). Tool use via Messages API                                              |
| google.golang.org/genai | v1.71.0 | Second provider (Gemini), behind the same interface — only if Q-01 picks it                                    |
| OpenAI-compatible     | stdlib   | `openai_compat` provider is a thin HTTP client for self-hosted (Ollama/vLLM) — no SDK, only if Q-01 picks it  |
| golang.org/x/crypto   | v0.56.0  | argon2id (`argon2.IDKey`, t=3, m=64 MiB, p=4, 32-byte hash, 16-byte salt)                                     |
| google/uuid           | v1.6.0   | `uuid.NewV7()` for time-ordered ids                                                                            |
| caarlos0/env          | v11.4.1  | Env → config struct. Stable; low churn is fine here                                                            |
| shopspring/decimal    | pin in Phase 0 | Decimal math in services; verify latest at scaffold time                                                  |
| WebP encoding         | decide in Phase 2 | `golang.org/x/image` has no WebP encoder; the Phase 2 task researches (`gen2brain/webp` or `libvips` via `bimg`) and records an ADR amendment |
| Not used              | —        | JWT (ADR-005 uses opaque sessions), Redis (no need for one shop; rate limits in-memory + Postgres), any ORM     |

## Contract and shared packages

| Item                | Pin      | Notes                                                                           |
| ------------------- | -------- | ------------------------------------------------------------------------------- |
| OpenAPI             | 3.1      | `contracts/openapi.yaml`; money/quantity as `string`, `format: decimal`          |
| openapi-typescript  | v7.13.0  | Types for `packages/api-client`                                                  |
| openapi-fetch       | v0.17.0  | 6 KB typed client used by web, admin and mobile                                  |
| i18next             | v26.4.1  | With `react-i18next` v17.0.13 in all three TS apps; JSON in `packages/i18n`      |
| ui-tokens           | ours     | `packages/ui-tokens`: colours, radius, spacing, font as CSS variables + JS export (D-25) |

Alternative considered for i18n: Paraglide (compiler-based, smaller). Rejected for MVP
because one runtime shared by web, admin **and** React Native keeps the agents on one
pattern; revisit if bundle size becomes a landing performance issue.

## Web — landing (`web/`) and admin (`admin/`)

| Item                     | Pin        | Notes                                                                                   |
| ------------------------ | ---------- | --------------------------------------------------------------------------------------- |
| Node.js                  | 24.20.0 LTS | `.nvmrc`/fnm; LTS until 2028-04                                                          |
| pnpm                     | v11.25.0   | Workspaces; **never npm/yarn**                                                          |
| TypeScript               | 7.0.2 (verify) | TS 7 is the current major (Go-native compiler). Phase 0 verifies TanStack Start, Vite 8 and Expo 57 accept it; otherwise pin the latest 6.x and note it here |
| React / react-dom        | 19.2.8     | Web apps. (Expo pins its own React — see Mobile)                                        |
| TanStack Start           | v1.168.49  | Landing framework, SSR, server functions (D-10). GA 1.x                                  |
| TanStack Router          | v1.170.32  | Both web apps (Start bundles it for `web/`)                                             |
| TanStack Query           | v5.102.8   | Server state in admin, web and mobile                                                   |
| Vite                     | v8.2.2     | Admin build; Start uses it under the hood                                               |
| Tailwind CSS             | v4.3.3     | Both web apps                                                                           |
| Ant Design (antd)        | v6.6.2     | **Admin UI kit** (D-25). React 19 native; `uz_UZ`/`ru_RU` locales verified 2026-09-03; theme via `ConfigProvider` from `packages/ui-tokens` |
| shadcn                   | v4.13.1 (CLI) | **Landing** primitives only (dialog, menu, language switcher); the rest is hand-written Tailwind for a distinctive look |
| Forms                    | —          | Ant `Form` in the admin; no react-hook-form, no zod (D-26)                              |
| lucide-react             | pin at T4  | Icon set shared with mobile (`lucide-react-native`)                                     |
| Inter (font)             | —          | Covers Cyrillic and Uzbek Latin (Oʻ, Gʻ); self-hosted, no Google Fonts call at runtime   |
| Biome                    | v2.5.11    | Lint + format, replaces ESLint/Prettier (O-07)                                          |
| Vitest                   | v4.1.11    | Unit tests                                                                              |
| Playwright               | v1.62.1    | e2e for admin and landing; also the `playwright` MCP for `/phase-done`                  |

## Mobile (`mobile/`)

| Item          | Pin      | Notes                                                                                   |
| ------------- | -------- | --------------------------------------------------------------------------------------- |
| Expo SDK      | 57.0.19  | React Native 0.86, **React 19.2.3** (Expo's pin, do not raise), Node ≥ 22.13           |
| expo-router   | 57.0.17  | File-based routing                                                                      |
| NativeWind    | v4.2.6   | Tailwind classes in RN                                                                  |
| react-native-reusables | pin in Phase 5 | shadcn-style copy-in components on NativeWind (D-25); verify version at Phase 5      |
| eas-cli       | v23.2.0  | Android APK builds (D-12); `expo-secure-store` for the session token                   |

## Infrastructure

| Item            | Pin      | Notes                                                                                                                   |
| --------------- | -------- | ----------------------------------------------------------------------------------------------------------------------- |
| PostgreSQL      | 18.6     | Current stable. PostgreSQL 19 is in beta (GA expected September 2026) — **do not adopt until an owner decision**        |
| Docker Compose  | v5.5.0   | Compose is a Docker CLI plugin; the `docker compose` command. (v2 naming is legacy)                                       |
| Caddy           | v2.11.4  | TLS, static files, reverse proxy, security headers                                                                       |
| Object storage  | none     | **MinIO's main repository was archived in April 2026.** MVP stores media on a disk volume (ADR-008). If S3 is needed later: SeaweedFS, Garage or RustFS |
| GitHub Actions  | —        | `ci.yml` from Phase 0, `deploy.yml` from Phase 8                                                                        |
| lefthook        | pin in Phase 0 | Git hooks (commit-msg conventional-commit + trailer ban) — chosen over husky: single binary, no Node dependency for Go-only commits |

## LLM for the bot (Q-01 open)

Cost reference from the Claude API pricing table (2026-06-24 cache): Haiku 4.5 $1/$5
per MTok in/out (200K context); Sonnet 5 $2/$10. A customer question with tools is
roughly 2–4K tokens in and 200 out, so Haiku is on the order of $0.005 per answer. Gemini
Flash-class models are comparable or cheaper; self-hosted has no per-token cost but needs
RAM/GPU on the VPS and someone to operate it. ADR-009 keeps all three behind one
interface so the choice can be made — and changed — by config.

## Tooling for agents

| Tool        | Version / notes                                                                              |
| ----------- | -------------------------------------------------------------------------------------------- |
| Claude Code | 2.1.x; models: Fable 5.1 (orchestrator), Sonnet 5, Opus 5, Haiku 4.5                         |
| context7    | MCP over HTTP; mandatory for verifying library APIs                                          |
| postgres MCP | **none** (D-24) — upstream deprecated 2025-07 with a read-only bypass; inspect with `docker compose exec postgres psql` |
| playwright MCP | `@playwright/mcp` for browser-driven verification                                          |
| agent-skills (vendored) | addyosmani/agent-skills @ 020ec10 (2026-09-03), 10 skills + linked references, MIT; see .claude/skills/VENDORED.md |
| gh          | 2.98, authenticated as Sulton-Ali                                                              |
| Go          | 1.27.0 (CachyOS build)                                                                        |
