# 03 — Architecture

## Monorepo layout

```
savdo/
├── api/                    Go module github.com/Sulton-Ali/savdo/api
│   ├── cmd/api/            HTTP API binary
│   ├── cmd/bot/            Telegram bot binary
│   ├── cmd/savdo/          Admin CLI (migrate, seed, reset-owner-password)
│   ├── internal/
│   │   ├── auth/           sessions, passwords, Telegram login, OTP, roles
│   │   ├── shop/           shop row, settings, locations, staff
│   │   ├── catalog/        categories, products, variants, units, translations
│   │   ├── media/          uploads, thumbnails, Storage interface
│   │   ├── stock/          movements ledger, levels, purchases, adjustments, transfers
│   │   ├── sales/          quick sale, void, return, payments, discounts
│   │   ├── crm/            customers, suppliers
│   │   ├── content/        landing content blocks, hours, contacts
│   │   ├── reports/        read-only aggregates
│   │   ├── ai/             LLM adapter interface + providers
│   │   ├── bot/            Telegram handlers, tools exposed to the LLM
│   │   ├── httpx/          router wiring, middleware, error mapping
│   │   └── db/             pgx pool, tx helpers, sqlc-generated code
│   ├── db/migrations/      goose SQL migrations
│   ├── db/queries/         sqlc SQL sources
│   └── gen/                oapi-codegen output (committed)
├── contracts/openapi.yaml  THE API contract (ADR-002)
├── packages/
│   ├── api-client/         openapi-typescript + openapi-fetch (generated, committed)
│   ├── i18n/               uz/ru/en JSON, shared by web, admin, mobile
│   └── ui-tokens/          colours, radius, font — Ant theme, landing CSS, NativeWind read it
├── web/                    TanStack Start — public landing (SSR)
├── admin/                  Vite + React SPA — admin panel
├── mobile/                 Expo — mobile admin (source in mobile/src/app)
├── infra/                  docker-compose.yml, Caddyfile, .env.example, backup script
├── docs/
├── .claude/                agents, skills, settings
├── Makefile                make verify / generate / dev-infra / migrate / seed
└── pnpm-workspace.yaml
```

## Module map (Go)

Each `internal/<module>` has the same shape:

```
service.go      business rules, transactions, permission checks
handler.go      implements the oapi-codegen ServerInterface slice for this module
queries.sql.go  sqlc output (in internal/db, imported)
errors.go       typed errors mapped to API error codes
service_test.go integration tests against testcontainers Postgres
```

Dependency direction: `handler → service → db`. Services may call other services
(`sales` calls `stock`); handlers never call another module's db layer.

| Module    | Owns tables                                                                   | Correctness-critical |
| --------- | ----------------------------------------------------------------------------- | -------------------- |
| `auth`    | `users`, `sessions`, `telegram_accounts`, `otp_codes`                         | **yes**              |
| `shop`    | `shops`, `locations`, `shop_settings`                                         | no                   |
| `catalog` | `categories`, `products`, `product_variants`, `units`, `*_translations`, `attribute_definitions` | no   |
| `media`   | `media_files`, `product_images`                                               | no                   |
| `stock`   | `stock_movements`, `stock_levels`, `purchases`, `purchase_items`              | **yes**              |
| `audit`   | `audit_log` (writer only, no handler)                                        | no                   |
| `sales`   | `sales`, `sale_items`, `sale_payments`, `discounts`                           | **yes**              |
| `crm`     | `customers`, `suppliers`                                                      | no                   |
| `content` | `content_blocks`                                                              | no                   |
| `reports` | none (reads)                                                                  | no                   |
| `ai`      | none                                                                          | **yes** (data boundary) |
| `bot`     | `bot_conversations`, `bot_messages`                                           | **yes** (data boundary) |

## Key flows

### Quick sale

1. Cashier posts `POST /sales` with `{locationId, customerId?, items:[{variantId, qty}], discount?, payment:{method}}`.
2. `sales.Service.Create` opens one transaction:
   - loads current prices and promo prices for each variant (never trusts client totals),
   - computes line totals and the sale total,
   - calls `stock.Service.Move(tx, ...)` with kind `sale_out` per line — this inserts
     `stock_movements` rows and updates `stock_levels`, failing if the level would go
     negative (unless the shop setting `allow_negative_stock` is on),
   - inserts `sales`, `sale_items`, `sale_payments` with status `completed`,
   - commits.
3. Response is the sale with computed totals. The sale is now immutable (ADR-014).

### Receive a purchase

`POST /purchases` creates a `draft` purchase with its items (number from `shops.next_purchase_number` under row lock, D-45). `POST /purchases/{id}/receive` → `stock.Service.ReceivePurchase` in one transaction: for each item a `purchase_in` movement at the purchase's location with the line `unit_cost` via `stock.Service.Move`, `total_cost` computed server-side, status `received`, an `audit_log` row (D-47). When `update_cost_on_purchase` is on, each received variant's `cost_override` is set to the line `unit_cost` (D-42, D-48). A received purchase is immutable; `POST /purchases/{id}/cancel` writes reversing movements under the same negative-stock rule.

### Landing render

TanStack Start server function → `GET /public/shop`, `/public/categories`,
`/public/products?…` (locale via `Accept-Language`). Public endpoints return **no**
cost price, no quantities — only `availability: in_stock | low | out_of_stock` per
variant. Cached 60 s at the API with ETag.

### Bot question

1. Telegram update → `cmd/bot` handler → loads/creates `bot_conversations` for the chat.
2. Rate limit per chat and per shop (ADR-009).
3. `ai.Client.Chat(system, history, tools)` with tools `search_products`,
   `variant_availability`, `shop_info` (hours, address, contacts). Tools call the same
   **public** read services the landing uses — nothing else exists to call.
4. Tool loop runs at most 5 rounds; answer stored in `bot_messages`; reply sent.
5. Every question/answer is visible to the owner in the admin (`/bot/conversations`).

### Auth

Password login → argon2id verify → new `sessions` row (random 32-byte token, stored as
SHA-256) → token returned as `Set-Cookie` (HttpOnly, Secure, SameSite=Lax) for web and
in the body for mobile (stored in SecureStore, sent as `Authorization: Bearer`). Every
request resolves `shop_id`, `user_id`, `role` from the session into the context; every
service takes them from there. The shop is resolved once at startup from `SHOP_SLUG` (O-11) and injected into the auth service; login looks the user up in that shop only. Behind the production proxy the client IP is the last `X-Forwarded-For` hop.

## Architecture Decision Records

Format: **Context → Decision → Consequences.** Reversing one is an owner decision.

### ADR-001 — One Go API, one monorepo, four clients

All clients (landing, admin, mobile, bot) talk to one Go HTTP API. No BFF, no GraphQL.
The bot is a second binary in the same Go module using the same services. Consequence:
one contract, one auth model, one place for business rules.

### ADR-002 — Contract first: OpenAPI 3.1 is the source of truth

`contracts/openapi.yaml` defines every path, schema and error code. `make generate`
produces Go server interfaces/types (`oapi-codegen`), and TypeScript types + a typed
fetch client (`openapi-typescript` + `openapi-fetch`) consumed by web, admin and mobile.
Generated code is committed; `make verify` fails if it is stale. Nobody declares a
request/response shape by hand. Workflow: `/api-change`.

### ADR-003 — PostgreSQL system of record; sqlc + goose; no ORM

Queries are SQL files compiled by sqlc into typed Go. Migrations are goose SQL files,
hand-written (there is no schema-diff tool in this stack, so review of migration SQL is
the gate), additive by default, never edited after merge. Enum types are real Postgres
enums. Every table: `id uuid`, `created_at`/`updated_at timestamptz`, `shop_id`.

### ADR-004 — Tenant-ready from day one

Every business table has `shop_id uuid not null references shops`. The auth context
carries `shop_id`; every sqlc query takes it as a parameter and filters by it. The MVP
seeds exactly one shop. Opening registration later is additive: a signup flow and a
billing module, no data migration. Unique constraints are `(shop_id, …)`, never global.

### ADR-005 — Opaque DB sessions; argon2id passwords; Telegram login and OTP

No JWT. Sessions are rows with a hashed token, `expires_at`, `last_seen_at`, and can be
revoked. Passwords hashed with argon2id (x/crypto). Telegram Login Widget (web) and bot
deep-link (mobile) bind a Telegram user id to a `users` row; both verify the Telegram
HMAC. OTP codes (6 digits, 5 min, single use, hashed) are delivered by the bot to the
user's linked Telegram account for password reset and sensitive changes. Before the bot
exists (Phase 7), the owner resets passwords from the admin (Q-11).

### ADR-006 — Stock is an append-only ledger

`stock_movements(shop_id, variant_id, location_id, kind, qty, unit_cost, ref_type,
ref_id, reason, created_by, created_at)` is the truth. `stock_levels(shop_id,
variant_id, location_id, qty)` is a materialized view maintained **in the same
transaction** by `stock.Service.Move`, with `SELECT … FOR UPDATE` on the level row to
serialize concurrent sales. A `savdo stock rebuild` CLI recomputes levels from
movements. Kinds: `purchase_in`, `sale_out`, `sale_void_in`, `return_in`,
`adjustment`, `transfer_out`, `transfer_in`. Nothing updates `stock_levels` directly. Amended 2026-09-04 (D-41, D-42): a level never goes below zero (`STOCK_INSUFFICIENT`); receiving a purchase sets the variant's `cost_override` to the line's `unit_cost`. Low-stock listing per D-50. Purchase cancellation reverses with negative `purchase_in` rows (D-51).

### ADR-007 — Money as `NUMERIC(14,2)` with shop currency

No floats anywhere. Go uses `shopspring/decimal` (pin in `02-TECH-STACK.md`) or
`pgtype.Numeric`; API sends money as **strings** (`"125000.00"`) to avoid JS float
loss. Currency is one per shop (`UZS` for MVP). Quantities are `NUMERIC(12,3)` so units
like kg work; clothing uses whole numbers.

### ADR-008 — Media on local disk behind a `Storage` interface

`media.Storage` has `Put`, `Get`, `Delete`, `URL`. The MVP implementation writes under
`/data/media/<shop_id>/…` on a Docker volume; Caddy serves `/media/*` directly. Go
generates 3 sizes (thumb 200, card 600, full 1600, WebP) on upload. An S3
implementation can be added later without touching callers. No MinIO (see
`02-TECH-STACK.md` for the maintenance-status note that motivated this).

### ADR-009 — Telegram bot with a provider-agnostic LLM adapter and a hard data boundary

`internal/ai` exposes `Client` with `Chat(ctx, req) (resp, error)` supporting system
prompt, messages, tool definitions, tool results and usage. Providers: `anthropic`
(first), `gemini`, `openai_compat` (any OpenAI-compatible endpoint, for self-hosted).
Provider and model are config (`AI_PROVIDER`, `AI_MODEL`). The customer-mode bot gets
only public-read tools (ADR "Bot question" flow). Rate limits: per chat 20 messages/hour,
per shop configurable daily token budget; over budget the bot falls back to static
answers (hours, address, "ask a human"). Every LLM call logs provider, model, tokens,
latency and cost estimate. Model choice: **Q-01**.

### ADR-010 — Roles and field-level permissions in the service layer

Roles: `owner`, `manager`, `cashier`. The permission matrix (`04-DATA-MODEL.md`
§ Permissions) is enforced by middleware per route **and** by services for fields
(`cost_price`, margin, reports). The UI hides what a role cannot do, but the API is the
enforcement point. Public endpoints run with a `public` role that sees the least.

### ADR-011 — Landing is SSR from the public API; content is admin-managed

`web/` uses TanStack Start server functions to fetch `/public/*` and renders HTML with
per-locale meta. Shop content (hero, about, hours, contacts, social) lives in
`content_blocks` edited from the admin. The landing has no database access and no
secrets.

### ADR-012 — i18n: UI strings in JSON, data translations in DB

`packages/i18n/{uz,ru,en}.json` is the single UI dictionary for web, admin and mobile
(react-i18next in all three). Product/category names and descriptions live in
`*_translations(entity_id, locale, …)` with fallback order `requested → uz → any`.
API responses honour `Accept-Language` and return `locale` and `translationFallback`.

### ADR-013 — Machine-readable error codes

Every error response is `{ "error": { "code": "STOCK_INSUFFICIENT", "details": {…} } }`.
Codes are an enum in the OpenAPI spec; clients translate them. A human sentence in an
API response is a review finding.

### ADR-014 — Completed sales are immutable

A `sales` row with status `completed` is never updated except to `voided`. A void
writes `sale_void_in` movements for every line. A return is a new `returns` sale kind
referencing the original, with `return_in` movements. Sale numbers are per shop,
sequential, gap-free within a shop (`shops.next_sale_number` under row lock).
Amended 2026-09-05 (D-58, D-59, D-61, D-62, D-66): voids are limited to the sale's calendar day in the shop timezone and are refused once a return exists; returns may be partial, refund by the original payment method, and refund each line net of its proportional share of the sale discount. Returns are final and cannot be voided (D-66).
Amended 2026-09-06 (D-87): draft sales live in separate mutable tables `sale_drafts` and `sale_draft_items` outside the `sales` ledger; a draft stores location, lines, customer, discount and notes with no sale number. Completing a draft creates the sale via `sales.Service` in the same transaction as the draft's deletion, under `Idempotency-Key`. Immutability and void/return rules apply from completion onward; until then, a draft is mutable by its creator or manager+.

## Cross-cutting

- **Logging**: `log/slog`, JSON, request id per request, never log bodies of auth
  requests or tokens.
- **Config**: environment variables parsed once at startup into a struct; missing
  required values fail fast.
- **Health**: `GET /healthz` (process) and `GET /readyz` (DB reachable).
- **Rate limiting**: login and OTP endpoints per IP and per username; bot per chat.
- **Time**: everything UTC in DB; shop timezone (`Asia/Tashkent`) applied only for
  display and for "today" in reports.
- **IDs**: UUID v7 (time-ordered) generated in Go.
