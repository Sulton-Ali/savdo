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

- [x] Owner answered Q-11 (D-28), session policy (D-29), seed content (D-30), dependencies (D-31) — 2026-09-04
- [x] Config, slog logging, request id, error mapping, `/readyz`
- [x] Migrations 0001–0003: `shops`, `users`, `sessions`, `locations`, enums
- [x] `auth`: argon2id passwords, opaque sessions, login/logout/me, rate limit on login
- [x] Role middleware (owner/manager/cashier) and permission helper
- [x] `shop`: get/patch shop, locations CRUD, staff CRUD + password set (owner)
- [x] `savdo` CLI: `migrate`, `seed`, `reset-owner-password`
- [x] Seed: one shop (family clothing shop placeholder), owner, one manager, one cashier, two locations
- [x] Integration tests with testcontainers: login, role denial, shop isolation
- [x] Admin web shell: login page, session handling, layout, nav, i18n switcher, staff and locations screens

**Done when:** owner logs in on the admin web, creates a cashier, the cashier logs in
and is denied `/staff`; `curl` with a bogus token gets `401 UNAUTHENTICATED`; the
integration test proving a query with the wrong `shop_id` returns nothing is present and
green.

---

## Phase 2 — Catalogue (products, variants, categories, media) + admin screens

- [x] Owner answers Q-03, Q-04, Q-12, Q-16, Q-20 (answered 2026-09-04: D-32..D-37)
- [x] Migrations: `units`, `categories(+translations)`, `attribute_definitions`, `products(+translations)`, `product_variants`, `media_files`, `product_images`
- [x] `catalog` service + handlers per `05-API.md` § Catalogue; cursor pagination; `?q=` search
- [x] `media`: upload, validation (mime, size), WebP derivatives, local `Storage`, Caddy path
- [x] Role-shaped product schemas (staff vs cashier) in the spec
- [x] Admin: categories tree, product list/search, product form with variants matrix (size × colour), image upload and ordering, translations tabs
- [x] Seed: ~30 clothing products with variants and placeholder images
- [x] Tests: variant uniqueness, translation fallback, cashier never receives `costPrice`

**Done when:** the owner creates a jacket with 3 sizes × 2 colours and 4 photos in the
admin, edits its Russian name, and a cashier session sees it without cost price; the
public schemas exist in the spec even though the landing is not built yet.

---

## Phase 3 — Stock ledger, purchases, suppliers + admin screens (correctness-critical)

- [x] Owner answers Q-02
- [x] Migrations: `suppliers`, `purchases`, `purchase_items`, `stock_movements` (+ append-only trigger), `stock_levels`, `audit_log` (D-47)
- [x] `stock.Service.Move` with `FOR UPDATE` and negative-stock rule; `savdo stock rebuild`
- [x] Purchases: draft → receive → movements; cancel with reversal
- [x] Adjustments (with reason) and transfers between locations
- [x] Stock levels and movement history endpoints, low-stock endpoint
- [x] `PATCH /products/{id}/images/{imageId}` (variant retag / cover) and admin retag flow (Q-21, D-43)
- [x] Admin: suppliers, purchase form, receive flow, stock levels grid per location, movement history, adjustment and transfer forms
- [x] Seed: suppliers and received purchases giving the demo catalogue opening stock, one transfer to the storeroom (D-49)
- [x] Tests: concurrent sales on the last unit (only one succeeds), rebuild equals levels, trigger blocks UPDATE/DELETE on movements
- [x] Two-model review (Sonnet + Opus) recorded in merge bodies

**Done when:** receiving a purchase of 10 units, transferring 4 to the storeroom and
adjusting −1 shows 5/3 per location; `savdo stock rebuild` reproduces the same numbers
after truncating `stock_levels`; the concurrency test is green.

---

## Phase 4 — Sales, customers, discounts, reports + admin screens (correctness-critical)

- [ ] Owner answers Q-05, Q-06, Q-07, Q-10
- [ ] Migrations: `customers`, `sales`, `sale_items`, `sale_payments`
- [ ] Quick sale with server-side pricing, promo price, discount, idempotency key, per-shop sale number
- [ ] Void and return with reversing movements; immutability enforced
- [ ] Customers CRUD with purchase history
- [ ] Reports: sales summary (period), by product, low stock; cashier own-day rule
- [ ] Admin: quick-sale screen (search, variant pick, cart, payment), sales list/detail, void/return, customers, reports dashboard
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
- [ ] Least-privilege database role for the API (no TRUNCATE / no trigger disable on the ledger); the append-only trigger on `stock_movements` covers UPDATE/DELETE only.
- [ ] Owner acceptance week (see `01-OVERVIEW.md` § Definition of done)

**Done when:** the shop has run one full week on Savdo in production and the six
definition-of-done points hold.

---

## Backlog (post-MVP, unordered)

Debt ledger (nasiya) · barcode scanning in the mobile app · online cart + checkout with
Payme/Click · multi-tenant signup and billing · iOS build · loyalty points · SMS OTP ·
offline mobile mode · bot staff mode · printed receipts · campaign discounts (automatic percent/sum by category, product or all sales for a date range; D-60) · accounting exports.

Engineering debt: Move the `Accept-Language` context helpers out of `catalog` into a small `internal/locale` package so `stock` (and later `sales`) stop importing a sibling domain module · Idempotency helper: run the permission check and request validation before opening the transaction and taking the advisory lock; re-run `auth.Require` on replay · Movement history: add a `createdByName`-style display name to purchase and adjustment audit views; per-row quick actions on the stock levels grid.

## Phase log

| Phase | Closed | Verified by | Deferred |
| ----- | ------ | ----------- | -------- |
| 0 — Bootstrap | 2026-09-04 | fresh clone, make dev-infra && make verify exit 0, healthz 200, admin proxy + shell, web SSR "API: ok", Expo Metro boot + Android export, CI run 33800653774 success | On-device mobile screen verified by the owner with Expo Go at acceptance; lucide-react pin to Phase 1; jest-expo (Q-17), brand colour (Q-16), Android package id (Q-18) open |
| 1 — Core API + admin shell | 2026-09-04 | fresh clone (`f672a8a`), make verify exit 0, isolation tests green (`TestListLocationsIsolation`, `TestListLocationsQueryLevelIsolation`), bogus token → 401 `UNAUTHENTICATED`, owner→cashier→403 flow via the API with the admin's own request shape (`client:"web"` cookie + `X-Requested-With`), cashier sessions revoked on deactivate (401 after `isActive:false`), 409 `field:username` on duplicate staff, cursor paging on `/locations`, admin Vitest 25 tests (12 files) + build green, CI run 33826222743 success | Browser click-through of the admin login/staff screens verified by the owner at acceptance (seeded accounts); Q-19 login abuse horizon and Q-20 nullable PATCH fields open; lucide-react installed; Caddy trusted_proxies and ENV=prod wiring to Phase 8 |
| 2 — Catalogue + admin screens | 2026-09-04 | 9 merges on `main` (`3357ade` t1-contract, `849a776` t2-schema-catalog, `271d528` t3-media, `89c7c03` t4-catalog, `6b693f8` t5-seed, `8f6a4bb` t6a-admin-catalog, `ca50a5a` t6b-admin-variants-images, `bfdee8b` t7-brand-tokens, `cb08fc3` t8-docs); Done-when bar run live against the API on `:18123` from the main checkout's `api/` (default `MEDIA_DIR`, real Postgres, seeded `savdo-demo`): owner login → `POST /products` created a jacket with 6 variants (3 sizes × 2 colours) → 4 PNGs generated locally → `POST /media` ×4 (each converted to WebP thumb/card/full) → `POST /products/{id}/images` ×4 attached (1 cover + 3) → `PATCH /products/{id}` `{translations:{ru:{...}}}` set the Russian name (uz entry untouched, `GET ?Accept-Language=ru` resolved `locale:"ru"`, `translationFallback:false`) → cashier login → `GET /products/{id}` returned 200 with all 4 images and 6 variants but no `costPrice`, no `translations`, and no `costOverride` on any variant; cleaned up after (4 images unlinked, product soft-deleted → 404, the 4 `media_files` rows and on-disk WebP files deleted directly — no delete-media endpoint exists by design, media is content-addressed); `products`/`product_variants`/`product_images`/`media_files` counts back to the seed baseline (30/135/61/61) aside from 6 orphaned variant rows left under the soft-deleted test product, matching the literal cleanup steps run. Task-box evidence: migrations `0004`–`0008`; `catalog` cursor pagination (`nextCursor`) and `?q=` ILIKE search (`api/internal/catalog/products.go`); media upload/validation/WebP path (T3 live smoke); role-shaped `Product`/`Variant` vs `ProductPublic`/`VariantPublic` in `contracts/openapi.yaml`; admin components present (`VariantMatrixGenerator.tsx`, `VariantsImagesTab.tsx`, `VariantsTable.tsx`, `ImageCropModal.tsx`, `CategoriesPage.tsx`); seed at 30 products/135 variants/61 images pre-test; `go test` green on `TestProductVariants_attributesUniquePerProduct`, `TestGetProduct_translationFallback`, `TestProducts_cashierResponseHasNoCostOrTranslationsKeys`, `TestListProductsForCashierAndPublic_haveNoCostFields`, `TestProducts_variantCostGatedOnPermissionNotOnProductCostValue`. Review findings per merge body (no separate severity-tagged report is stored in git for T4/T5/T6b/T8 — smoke-tested only): T1 5 fix-round findings + 2 MINOR deferred; T2 5 fix-round findings; T3 (correctness-critical, reviewed independently by two sessions on different models — Sonnet + Opus — across 3 rounds) 9 findings fixed, several CRITICAL-grade (decompression bomb via unbounded decode, symlink traversal in dev media serving, EXIF/originals served instead of stripped derivatives, unbounded spool/decode queues) + 3 MINOR deferred; T6a 4 fix-round findings; T7 approved with 2 MINOR deferred. Two post-merge fixes folded in: `408adad` (`, id` tiebreaker on `ListVariantsForStaff`/`ListVariantsForCashier` — every variant of one product is inserted in the same transaction, and Postgres `now()` is transaction-time, so `ORDER BY created_at` alone left ties unordered) and `c0b1689` (seed's `attachImages` matched a variant-tagged image by attribute value instead of positional index, for the same created_at-tie reason — a positional match had silently tagged a shirt's 2nd image with the wrong size). `pnpm exec biome ci .` clean; `bash scripts/guards.sh` all PASS. | Admin UI (variants matrix, images gallery, crop, retag) verified by unit tests and build only; browser acceptance by owner pending. Q-21 open: `PATCH /products/{id}/images/{imageId}` (variant retag/cover without remove→add→reorder) not built this phase. `?limit=` clamping to 1–200 without rejecting is by design (`05-API.md`), not a gap. Correctness lesson carried forward: never order by `created_at` alone for rows inserted in one transaction — add a tiebreaker (`408adad`) or match by natural key (`c0b1689`). |
| 3 — Stock ledger, purchases, suppliers + admin screens | 2026-09-05 | 16 merges on `main` between `6043689` and `592991b` (14 phase-3-scoped: `db9b44d` interview, `7bfa58c` t0-docs, `049db3c` t1-contract, `dd15ba2` t2-schema, `5bccc30` t2b-docs-low-stock, `88c6c1b` t3-stock-core, `f4f878e` t4-purchases, `3ae8f56` t5-image-patch, `a55b323` t6a-admin-purchases, `221008d` t6b-admin-stock, `7bf5f92` t6c-admin-purchase-labels, `37511df` t7-seed, `382df2f` t8-docs, `592991b` t9-threshold-wiring; plus 2 leftover Phase-2 acceptance-fix merges `08899ef`/`373dbd9`). Done-when bar exercised live against the running dev API (`:8080`, dev Postgres at migration 14) from this session's own worktree, reusing the shared `infra-postgres-1` container/volume (`infra_postgres-data`) and `infra/.env` recreated via `make dev-infra` from the tracked `.env.example` defaults (container was recreated by compose due to per-worktree working-dir labels; same named volume, health and data confirmed intact afterwards — 167 stock movements, migration 14, healthz 200 before and after): manager login (`POST /v1/auth/login`) → `GET /v1/locations` (MAIN = Doʻkon isDefault, STORE = Ombor) and `GET /v1/suppliers` → created a fresh product+variant (`POST /v1/products`, no prior stock) → `POST /v1/purchases` (draft, 10 units @800000.00) → `POST /v1/purchases/{id}/receive` with `Idempotency-Key` (200, `status:"received"`) → `GET /v1/stock/levels?variantId=` showed MAIN 10.000 → replaying `receive` with the **same** key returned an identical body and `GET /v1/stock/movements` still showed exactly 1 `purchase_in` row → `POST /v1/stock/transfers` moved 4 units MAIN→STORE (6/4) → `POST /v1/stock/adjustments` −1 at MAIN and −1 at STORE (each with its own Idempotency-Key, `reason:"count_correction"`) → `GET /v1/stock/levels?variantId=` returned exactly `{MAIN: "5.000", STORE: "3.000"}`, matching the bar's 5/3. Rebuild: snapshotted all 149 `stock_levels` rows (`select variant_id, location_id, qty ... order by 1,2`) to a file, ran `go run ./cmd/savdo stock rebuild --shop-slug savdo-demo` (output: `stock rebuild: shop "savdo-demo": 149 stock_levels rows rebuilt from 172 stock_movements rows`, which truncates and rewrites the shop's levels inside its own transaction per the CLI's own doc comment — the roadmap bar's "after truncating stock_levels" phrasing describes what the CLI does internally, not a separate manual step), snapshotted again: `diff` was empty, and the test variant's rows still read 5.000/3.000 after rebuild. Correctness-critical tests, run fresh (`-count=1`, `-race` where specified), no `t.Skip` in any of these files: `go test -race ./internal/stock/ -run 'Concurr|concurrent|Rebuild'` → all 8 tests PASS including `TestMove_concurrentLastUnit_exactlyOneSucceeds`, `TestReceivePurchaseTx_concurrentReceiveExactlyOneSucceeds`, `TestRebuild_matchesLedgerSum`, `TestCreateStockTransfer_opposingConcurrentTransfersDoNotDeadlock`; `go test ./internal/db/ -run 'AppendOnly\|appendOnly\|Rebuild'` → `TestStockMovements_appendOnlyRejectsUpdateAndDelete` and `TestStockRebuild_matchesLedgerSum` PASS (trigger raises on both UPDATE and DELETE). `make verify` from this worktree: exit 0 (format/lint/typecheck/generate-check/Go tests incl. testcontainers/pnpm -r test [admin 34 files/128 tests, web, packages]/guards.sh, all green; `pnpm -r test:e2e` does not exist in any package.json — confirmed absent, not run). `bash scripts/guards.sh`: all 5 PASS. Task boxes verified from code + this run: migrations `0009`–`0014` present incl. the `stock_movements_immutable` trigger (`0012_stock_ledger.sql`) rejecting `UPDATE`/`DELETE`; `stock.Move` (`internal/stock/move.go`) uses `GetLevelForUpdate` (`SELECT ... FOR UPDATE`, `db/queries/stock.sql`) and the `allow_negative_stock` shop-row rule; `savdo stock rebuild --shop-slug` (required flag, no default) confirmed live; purchases draft→receive implemented and exercised; cancel-with-reversal (`purchase_in`, negative qty, `ref_type:"purchase_cancel"`, no new ledger enum, D-51) confirmed in code (`internal/stock/purchases.go`) and by `TestCancelPurchase_receivedWritesReversingMovementsAndRebuildMatches` (PASS); adjustments-with-reason and transfers exercised live; all five `/stock/{levels,movements,adjustments,transfers,low}` endpoints present in the contract, four exercised live; `PATCH /products/{id}/images/{imageId}` present in the contract and implemented (`internal/catalog/images.go`), admin retag flow present (`admin/src/routes/app/ImageGallery.tsx` + test); admin screens present for Suppliers/Purchases/Stock (`SuppliersPage`, `PurchasesListPage`, `PurchaseFormPage`, `StockLevelsPage`, `StockMovementsPage`, `StockLowPage`, `StockActionsDrawer`) each with a passing `__tests__` file, all included in the 128 green admin Vitest tests; seed idempotent on `savdo-demo` (`go run ./cmd/savdo seed` → `stock already seeded`, confirming the original D-49 creation output `stock: 3 suppliers, 6 purchases, 135 purchase items, 10 transfers created` quoted in the t7-seed merge body is stable); tests box satisfied by the correctness-critical suite above; two-model review (Sonnet + Opus) confirmed in `git log --format=%B -1` for `dd15ba2` (T2 schema — Opus reviewed the ledger core), `88c6c1b` (T3 stock-core — Opus found a BLOCKER deadlock + a MAJOR double-write, both fixed), `f4f878e` (T4 purchases — Opus found 2 MAJORs: locale not honoured for `productName`, no multi-item purchase test, both fixed); Owner-answers-Q-02 box satisfied by D-40 in `00-DECISIONS.md` (Q-02 closed). One accidental side effect from this verification run, left uncorrected: probing the CLI's `--shop-slug` validation with an unknown slug (`does-not-exist`) triggered `seed.Seed`'s create-if-missing behaviour and produced a second shop with its own suppliers/purchases/6/172-row ledger; because the append-only trigger blocks `DELETE` on `stock_movements` (by design, ADR-006) and the t7-seed merge body itself records that "the ledger trigger blocks per-shop deletes, a full local reset recreates the database", this shop cannot be cleanly removed without either disabling the trigger (a ledger-integrity change requiring owner approval, not taken) or a full dev-database volume reset (would also erase the existing Phase 2/3 review leftovers, not taken unilaterally); it is shop_id-isolated from `savdo-demo` and did not affect any bar evidence above, but it is now permanent clutter in the shared dev Postgres volume pending an owner decision. The Phase-3 test fixture created for the bar itself (one product + variant on `savdo-demo`, purchase `P-000010`) was cleaned up the same way as the Phase 2 precedent: the product was soft-deleted via `DELETE /v1/products/{id}` (204); its ledger movements and the purchase row remain, matching the already-accepted "review leftovers ... which is fine" precedent from `P-000007`–`P-000009`. | Admin UI acceptance in the browser by the owner still pending (unit tests + build only this phase too; Phase 2 acceptance also still partly open). Contract gap: `lowStockThreshold` on `Shop`/`Product`/`ProductCreate`/`ProductPatch` lacks `minimum: 0` — needs a later `/api-change` (noted in `049db3c` and `592991b`). Missing test: a cashier-row constructor test for `lowStockThreshold` (`592991b`). Audience split for `Product.lowStockThreshold` deferred to before the public catalogue lands (Phase 6). Admin shows three different variant-label formats across the levels grid, the purchase form and the server's own `variantLabel` — not unified this phase. `StockLevelsPage` fetches one product per row (N+1 request pattern, client-cached, not fixed). A fresh seed leaves `/stock/low` empty — every seeded variant sits at or above the default threshold of 2. `internal/db`'s `ListMovements` query is superseded by `ListMovementsWithCreatedByName` and was left in place, unused, rather than deleted (out of scope for the branches that added the replacement). The admin has no retry affordance for the documented deadlock 409 on `/stock/transfers`. Backlog-only, not built: `internal/locale` package extraction (locale helpers still live in `internal/catalog`, imported by `internal/stock`); the idempotency helper still takes its transaction/advisory lock before the permission check and request validation, and a replay does not re-run `auth.Require` (both flagged in the T3 merge body as deferred to Phase 4's sales idempotency reuse); movement history's `createdByName` display name and per-row quick actions on the stock levels grid (both pre-existing backlog items, untouched). The extra "does-not-exist" shop created by this close's own verification run (see Verified-by) is left in the shared dev database pending an owner decision on whether to disable the ledger trigger for cleanup or reset the dev volume — flagged to the owner, not decided here. |
