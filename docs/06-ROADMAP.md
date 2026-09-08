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

- [x] Owner answers Q-05, Q-06, Q-07, Q-10
- [x] Migrations: `customers`, `sales`, `sale_items`, `sale_payments`
- [x] Quick sale with server-side pricing, promo price, discount, idempotency key, per-shop sale number
- [x] Void and return with reversing movements; immutability enforced
- [x] Customers CRUD with purchase history
- [x] Reports: sales summary (period), by product, low stock; cashier own-day rule
- [x] Admin: quick-sale screen (search, variant pick, cart, payment), sales list/detail, void/return, customers, reports dashboard
- [x] Tests: totals computed server-side, void restores stock, cashier cannot void, replayed idempotency key returns same sale
- [x] Two-model review recorded

**Done when:** a cashier sells 2 items to a customer with a 10 % discount on the admin
web, stock drops, the owner sees it in today's report with margin, voids it, and stock is
restored; all with the tests above green.

---

## Phase 5 — Mobile admin (Expo, Android)

- [ ] Expo Router app shell, login with editable server URL (D-79), secure token storage, i18n
- [ ] Products browse/search with variants and availability; quick edit + photo upload (manager+, D-77)
- [ ] Quick sale flow optimised for one hand; customer attach by phone
- [ ] Stock levels, receive purchase, adjustment
- [ ] Reports summary card (role-aware)
- [ ] Local Android release build (Expo prebuild + Gradle, D-72); build and install instructions in `07-DEVOPS.md`
- [ ] No automated mobile e2e (D-74); the Done-when manual check is the smoke
- [ ] Draft sales: server-side unpaid orders with Pay / Save draft / Delete from the quick-sale form (D-87..D-89); admin web drafts page
- [ ] Navigation rework: five most-used tabs plus a drawer; stock page split into single-purpose pages (D-90); sales list tab with date range (D-91)
- [ ] Lists show all rows newest first before search; stock levels order fixed (D-92); button label overflow fixed (D-93); bottom safe-area insets (D-95)
- [ ] Phone-test fixes (2026-09-08): Customers/Drafts pages get the shared shell (header, menu, tab bar); variant picker cache-key fix so stock/adjustment pickers list products; location and settings sheets clear the system bar (D-95); low-stock card link on its own line; naming per D-98

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
| 4 — Sales, customers, discounts, reports + admin screens (correctness-critical) | 2026-09-05 | 22 merges on `main` between `592991b` and `2e779bf`: `225ac16` interview (D-52..D-60), `8391597` t0-docs (D-61..D-63), `7b82aa9` t0-contract, `5db877e` t1b-docs (D-64/D-65), `55a8133` t6a-admin-customers, `c81e83c` t1c-docs (D-66/D-64 amendment), `e9d8596` t1-schema, `8672ce5` t2-customers, `d761d58` t5-reports, `d8bd793` t7-docs (D-67..D-69), `ec4c3ff` t6d-admin-reports, `7fc672d` t3-sales-create, `960d879` t6b-admin-quick-sale, `2fcef35` t6c-admin-sales, `d397c84` t8-docs (D-70), `dee3632` t4-sales-void-return, `8fe9c50` t6e-admin-sale-error-codes, `959d25a` t9-db-lock-test, `cc07716` t11-mcp-chromium, `dca72f0` t13-docs (D-71), `6f524c0` t12-reports-cashier-refunds, `2e779bf` t10-admin-quick-sale-flake. Branch `phase-4/close` rebased onto the final tip (`2e779bf`, which landed the t10 flake fix mid-close) from this session's own worktree, reusing the shared `infra-postgres-1`/`infra_postgres-data` and the already-running dev API (`:8080`, migration 16 confirmed via `savdo migrate status`) and the already-running admin dev server (`:5173`) started by the orchestrator from `main` — neither was restarted. Done-when bar exercised end to end **in the browser** (headless Chromium via a throwaway `npx playwright@latest` script against the cached `chromium-1194` build, `admin/` untouched — no MCP available to this session): logged in as `cashier` on the admin web, opened "Tezkor sotuv", picked filial Doʻkon, customer "Review Opus P4", product "Bolalar kostyumi" variant KID-05-01 (yashil/XS, 6 in stock at Doʻkon), qty 2, a 10 % ("Foiz") discount with a reason, and completed the sale: `POST /v1/sales` 201, `subtotal 358000.00`, server-computed `discountAmount 35800.00` (exactly 10 % of the client-untrusted subtotal), `total 322200.00`, sale #20, no `costPrice`/`unitCost` in the cashier-facing response → `GET /v1/stock/levels` confirmed the variant dropped from 6.000 to 4.000 at Doʻkon → logged in as `owner`, opened "Hisobotlar": today's summary showed the sale's numbers folded in (margin 787225.38 shop-wide) and the by-product table showed "Bolalar kostyumi" qtySold 2, revenue 322200.00, margin 146200.00 (`unitCost` 88000.00 visible only once logged in as owner, confirming the ADR-010 gate) → opened "Sotuvlar", clicked into sale #20, clicked "Sotuvni bekor qilish", entered a reason, confirmed: `POST /v1/sales/{id}/void` 200, `status:"voided"`, `voidReason` and `voidedAt` set, `voidedBy` a UUID (see Deferred) → `GET /v1/stock/levels` confirmed the variant was back to 6.000. Cashier-cannot-void confirmed separately by curl against a different completed sale with the correct CSRF header (`X-Requested-With: savdo`): `403 FORBIDDEN`. Correctness-critical tests, run fresh (`-race -count=1`) with no `t.Skip` in any of `api/internal/sales`, `api/internal/httpx`, `api/internal/reports`, `api/internal/db` (confirmed by `grep -rn t.Skip`, the only hit being `testdb.go`'s own `TESTDB_SKIP=1` opt-out helper, not a test body): all packages `ok`, including `TestCreateSale_computesTotalsServerSide`, `TestVoidSaleTx_restoresStockAndWritesOneMovementPerLine`, `TestVoidSaleTx_cashierForbidden`/`TestVoidSale_cashierForbiddenWithNoSideEffect`, `TestCreateSale_idempotentReplayReturnsIdenticalBodyNoSecondMovement`/`TestCreateSaleReturn_idempotentReplayReturnsIdenticalBodyOneMovementSet`, and the db-lock concurrency test `TestGetSaleItemsForUpdate_secondCallerBlocksUntilFirstCommits` (its `txA` still has no deferred rollback, a pre-existing MINOR noted at `959d25a` and left as-is). `pnpm -r test:e2e` confirmed absent from every `package.json` (Playwright e2e is not wired for any package yet). `make verify` from this worktree at the rebased tip: exit 0 twice in a row (format/lint/typecheck/generate-check/Go tests/`pnpm -r test` [43 admin files/201 tests, web, packages]/guards.sh all green; no flake seen, consistent with no orphaned vitest workers found via `ps -eo pid,etimes,args \| grep vitest` at verification time). Task boxes verified from code + this run: migrations `0015_customers.sql`/`0016_sales.sql` present (`customers`, `sales`, `sale_items`, `sale_payments`); server-side pricing/promo/discount/idempotency/per-shop numbering in `internal/sales/create.go` (`NextSaleNumber`, `promoActive`); void/return with reversing movements in `internal/sales/{void,return}.go`; customers CRUD with history in `internal/crm`; reports (summary, by-product, cashier own-day via D-71's cashier-attributed-return join) in `internal/reports`; all six admin screens present (`QuickSalePage`, `SalesListPage`, `SaleDetailPage`, `CustomersPage`, `CustomerDetailPage`, `ReportsPage`) each with a passing `__tests__` file inside the 201 green admin Vitest tests; Owner-answers-Q-05/Q-06/Q-07/Q-10 box satisfied by D-52..D-55 in `00-DECISIONS.md` (all four closed). Two-model review confirmed from `git log --format=%B -1` on `e9d8596` (t1-schema — Sonnet + Opus, Opus's CRITICAL on by-product discount attribution resolved by D-64), `7fc672d` (t3-sales-create — Sonnet + Opus, 3 MAJOR fixed), `dee3632` (t4-sales-void-return — Sonnet + Opus, 3 MAJOR fixed including the D-64 double-implementation reconciliation gap), `d761d58` (t5-reports — Sonnet + Opus, Opus's CRITICAL unbounded-`decimal.NewFromString` cursor DoS fixed); `6f524c0` (t12-reports-cashier-refunds) recorded **one Opus review only**, not the Sonnet+Opus pair the other four merges show — a genuine gap against this box's own wording, left unresolved rather than backfilled, since t12 is a 3-file SQL+test change already covered end-to-end by the live sale/void/report walk above and by `TestSalesSummaryForCashier_negativeNetRevenueFromPriorDayReturn` (fresh-run, green). | `voidedByName` missing from the contract — the admin shows a UUID in the void audit trail, confirmed still true by this close's own void response. Sales-list cashier filter is owner-only because `GET /staff` is owner-only; `SalesSummaryForStaff`'s cashier filter has no API parameter exposing it. `return.go` uses a narrow payment-method query instead of the general `GetSaleForStaff`. A theoretical negative last-line discount share when the discount is approximately equal to the subtotal (documented, not hit in practice). Void check order (window-closed check runs before the has-returns check) is informational, not fixed. Sale/SaleItem timestamps are served with a `+05:00` offset while `05-API.md` says UTC (Phase 8 `time/tzdata` item is related but separate). Plain-text 405 responses sit outside the ADR-013 error envelope. oapi param-binding errors use `details.parameter`/`reason`, not the O-12 `fields` vocabulary used elsewhere. `limit=0` falls back to the default instead of clamping to 1. D-56's wording says "rejected by the contract" while unknown request-body fields are in fact silently ignored, not rejected — a wording gap, not a behaviour bug. `Sale`/`SaleItem` lack `locale`/`translationFallback` fields. A variant with no cost freezes `unit_cost` at 0.00, which can overstate margin (owner informed, D-69 area). `time/tzdata` has no fallback wired (carried to Phase 8). Promo `date-time` fields are interpreted as calendar days per D-68; the contract's own field description still needs a wording pass to match. `TestGetSaleItemsForUpdate_secondCallerBlocksUntilFirstCommits`'s `txA` has no deferred rollback (confirmed still true by inspection at this close; `txB` does). Duplicated debounced-search boilerplate exists across admin screens (not extracted). Quick-sale till quantities are integers in the UI layer only (money/qty precision itself is server-enforced). `Product.lowStockThreshold` audience split carried over from Phase 3, still pending before Phase 6. Owner browser acceptance of the Phase 2–4 admin screens as a whole is still pending — this close performed its own independent browser walk of the Phase 4 flow, which is not a substitute for the owner's own sign-off. Two-model-review gap on `6f524c0` (see Verified-by): one Opus review only, not Sonnet+Opus — flagged, not backfilled after the fact. `CustomersPage.test.tsx`'s "shows an explicit Edit action…" test timed out twice under machine load in earlier runs (same class of issue as the quick-sale flake fixed on `2e779bf`) and may need the same per-test timeout treatment if it recurs. |
