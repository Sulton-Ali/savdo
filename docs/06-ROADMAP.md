# 06 — Roadmap

**Living state.** The first phase with an unchecked `- [ ]` box is the current phase.
Boxes are ticked **only** by `/phase-done` after the phase's **Done when** bar is
exercised for real. Mid-phase, `git log` is the truth about what merged; this file is the
truth about what was verified.

Each phase lists tasks (a route) and a Done-when bar (the destination). The bar wins.
Phases 2–4 are vertical slices: API **and** admin web screens for that module ship in
the same phase (O-05), so the owner can test each increment.

---

## Phase 0 — Bootstrap (docs, repo, toolchain, gate)

- [x] Discovery interview rounds 1–4 recorded in `00-DECISIONS.md`
- [x] `AGENTS.md`, `CLAUDE.md`, docs 01–08, agents, skills, MCP, settings
- [x] Owner answers Q-14 (GitHub) and Q-15 (Go install); remote `Sulton-Ali/savdo` exists (answered 2026-09-03; box ticked by /phase-done)
- [x] Monorepo scaffold: `api/` Go module with `cmd/api` hello server, `contracts/openapi.yaml` with `/healthz`, `packages/{i18n,api-client}`, `admin/`, `web/`, `mobile/` minimal apps that build
- [x] Root `Makefile`: `verify`, `generate`, `dev-infra`, `dev-infra-down`, `migrate`, `seed`, `api`, `bot`
- [x] `make generate` pipeline: oapi-codegen (Go) + openapi-typescript/openapi-fetch (TS) + sqlc, Go tools pinned via `go tool` in `go.mod` (D-24); freshness check in `verify`
- [x] `infra/docker-compose.yml` with Postgres (pinned), `infra/.env.example`, goose wired
- [x] Biome, golangci-lint, Vitest, Go test configs; `lefthook` commit-msg hook (Conventional Commits, rejects attribution trailers)
- [x] GitHub Actions `ci.yml` running `make verify` on PR and push to `main`
- [x] `.github/pull_request_template.md` with the merge checklist

**Done when:** on a fresh clone with Go, Node, pnpm and Docker installed, `make dev-infra
&& make verify` is green; `make api` serves `GET /v1/healthz`; `pnpm --filter admin dev`,
`pnpm --filter web dev` and `pnpm --filter mobile start` each boot a hello screen; CI is
green on `main`.

---

## Phase 1 — Core API + admin shell (auth, shop, staff, locations)

- [ ] Owner answered Q-11 (D-28), session policy (D-29), seed content (D-30), dependencies (D-31) — 2026-09-04
- [ ] Config, slog logging, request id, error mapping, `/readyz`
- [ ] Migrations 0001–0003: `shops`, `users`, `sessions`, `locations`, enums
- [ ] `auth`: argon2id passwords, opaque sessions, login/logout/me, rate limit on login
- [ ] Role middleware (owner/manager/cashier) and permission helper
- [ ] `shop`: get/patch shop, locations CRUD, staff CRUD + password set (owner)
- [ ] `savdo` CLI: `migrate`, `seed`, `reset-owner-password`
- [ ] Seed: one shop (family clothing shop placeholder), owner, one manager, one cashier, two locations
- [ ] Integration tests with testcontainers: login, role denial, shop isolation
- [ ] Admin web shell: login page, session handling, layout, nav, i18n switcher, staff and locations screens

**Done when:** owner logs in on the admin web, creates a cashier, the cashier logs in
and is denied `/staff`; `curl` with a bogus token gets `401 UNAUTHENTICATED`; the
integration test proving a query with the wrong `shop_id` returns nothing is present and
green.

---

## Phase 2 — Catalogue (products, variants, categories, media) + admin screens

- [ ] Owner answers Q-03, Q-04, Q-12
- [ ] Migrations: `units`, `categories(+translations)`, `attribute_definitions`, `products(+translations)`, `product_variants`, `media_files`, `product_images`
- [ ] `catalog` service + handlers per `05-API.md` § Catalogue; cursor pagination; `?q=` search
- [ ] `media`: upload, validation (mime, size), WebP derivatives, local `Storage`, Caddy path
- [ ] Role-shaped product schemas (staff vs cashier) in the spec
- [ ] Admin: categories tree, product list/search, product form with variants matrix (size × colour), image upload and ordering, translations tabs
- [ ] Seed: ~30 clothing products with variants and placeholder images
- [ ] Tests: variant uniqueness, translation fallback, cashier never receives `costPrice`

**Done when:** the owner creates a jacket with 3 sizes × 2 colours and 4 photos in the
admin, edits its Russian name, and a cashier session sees it without cost price; the
public schemas exist in the spec even though the landing is not built yet.

---

## Phase 3 — Stock ledger, purchases, suppliers + admin screens (correctness-critical)

- [ ] Owner answers Q-02
- [ ] Migrations: `suppliers`, `purchases`, `purchase_items`, `stock_movements` (+ append-only trigger), `stock_levels`
- [ ] `stock.Service.Move` with `FOR UPDATE` and negative-stock rule; `savdo stock rebuild`
- [ ] Purchases: draft → receive → movements; cancel with reversal
- [ ] Adjustments (with reason) and transfers between locations
- [ ] Stock levels and movement history endpoints, low-stock endpoint
- [ ] Admin: suppliers, purchase form, receive flow, stock levels grid per location, movement history, adjustment and transfer forms
- [ ] Tests: concurrent sales on the last unit (only one succeeds), rebuild equals levels, trigger blocks UPDATE/DELETE on movements
- [ ] Two-model review (Sonnet + Opus) recorded in merge bodies

**Done when:** receiving a purchase of 10 units, transferring 4 to the storeroom and
adjusting −1 shows 5/3 per location; `savdo stock rebuild` reproduces the same numbers
after truncating `stock_levels`; the concurrency test is green.

---

## Phase 4 — Sales, customers, discounts, reports + admin screens (correctness-critical)

- [ ] Owner answers Q-05, Q-06, Q-07, Q-10
- [ ] Migrations: `customers`, `sales`, `sale_items`, `sale_payments`, `discounts`, `audit_log`
- [ ] Quick sale with server-side pricing, promo price, discount, idempotency key, per-shop sale number
- [ ] Void and return with reversing movements; immutability enforced
- [ ] Customers CRUD with purchase history; discounts CRUD
- [ ] Reports: sales summary (period), by product, low stock; cashier own-day rule
- [ ] Admin: quick-sale screen (search, variant pick, cart, payment), sales list/detail, void/return, customers, discounts, reports dashboard
- [ ] Tests: totals computed server-side, void restores stock, cashier cannot void, replayed idempotency key returns same sale
- [ ] Two-model review recorded

**Done when:** a cashier sells 2 items to a customer with a 10 % discount on the admin
web, stock drops, the owner sees it in today's report with margin, voids it, and stock is
restored; all with the tests above green.

---

## Phase 5 — Mobile admin (Expo, Android)

- [ ] Expo Router app shell, login, secure token storage, i18n
- [ ] Products browse/search with variants and availability, product edit (manager+)
- [ ] Quick sale flow optimised for one hand; customer attach by phone
- [ ] Stock levels, receive purchase, adjustment
- [ ] Reports summary card (role-aware)
- [ ] EAS build profile for Android APK; install instructions in `07-DEVOPS.md`
- [ ] Playwright is not applicable; Maestro or Expo e2e smoke for login + sale (owner decides scope)

**Done when:** the owner installs the APK on an Android phone, logs in, makes a sale and
receives a purchase against the local/dev API, and the numbers match the admin web.

---

## Phase 6 — Landing (TanStack Start) + content management

- [ ] Owner answers Q-08, Q-09
- [ ] Migrations: `content_blocks`; content endpoints; public endpoints with caching/ETag
- [ ] Admin: landing content editor (hero, about, hours, contacts, social, SEO) per locale
- [ ] `web/`: home, category, product pages, about/contacts; SSR; per-locale routes; meta/OG; sitemap; availability badges
- [ ] Design pass: modern, fast, mobile-first; Lighthouse ≥ 90 on performance and SEO
- [ ] Playwright e2e: locale switch, product page renders availability

**Done when:** the landing renders the seeded catalogue in uz/ru/en with correct
availability, the owner changes the opening hours in the admin and the landing shows
them after refresh, and Lighthouse targets are met.

---

## Phase 7 — Telegram bot with AI, Telegram login, OTP

- [ ] Owner answers Q-01 (model), Q-13; provides bot token
- [ ] `internal/ai` adapter with `anthropic` provider first; `gemini` and `openai_compat` behind the same interface; usage logging
- [ ] `cmd/bot`: long polling (dev) / webhook (prod), `/start`, `/hours`, `/address`, `/catalog`, free-text Q&A via tools
- [ ] Data boundary tests: the tool set is exactly the public read set; a prompt-injection test asking for cost price gets none
- [ ] Rate limits and daily token budget with static fallback
- [ ] Migrations: `telegram_accounts`, `otp_codes`, `bot_conversations`, `bot_messages`
- [ ] Telegram Login (web widget + mobile deep link), OTP delivery, password reset flow
- [ ] Admin: bot conversations view
- [ ] Two-model review on `ai`/`bot` data boundary

**Done when:** ten scripted customer questions (availability, price, hours, address,
one adversarial) get correct answers from the real bot with the chosen provider; the
owner links Telegram and resets their password via OTP; conversations appear in the admin.

---

## Phase 8 — Production, onboarding the first shop

- [ ] VPS provisioned; Docker Compose prod file; Caddy with TLS on the real domain; env secrets
- [ ] GitHub Actions deploy workflow (SSH) with migrations run before the API starts
- [ ] Nightly `pg_dump` + media rsync off-box; restore drill performed and logged
- [ ] Monitoring: uptime check, error alerts to the owner's Telegram
- [ ] Import the family shop's real catalogue and opening stock (CSV import task or seed script)
- [ ] Security pass: headers, CORS, rate limits, dependency audit
- [ ] Owner acceptance week (see `01-OVERVIEW.md` § Definition of done)

**Done when:** the shop has run one full week on Savdo in production and the six
definition-of-done points hold.

---

## Backlog (post-MVP, unordered)

Debt ledger (nasiya) · barcode scanning in the mobile app · online cart + checkout with
Payme/Click · multi-tenant signup and billing · iOS build · loyalty points · SMS OTP ·
offline mobile mode · bot staff mode · printed receipts · accounting exports.

## Phase log

| Phase | Closed | Verified by | Deferred |
| ----- | ------ | ----------- | -------- |
| 0 — Bootstrap | 2026-09-04 | fresh clone, make dev-infra && make verify exit 0, healthz 200, admin proxy + shell, web SSR "API: ok", Expo Metro boot + Android export, CI run 33800653774 success | On-device mobile screen verified by the owner with Expo Go at acceptance; lucide-react pin to Phase 1; jest-expo (Q-17), brand colour (Q-16), Android package id (Q-18) open |
