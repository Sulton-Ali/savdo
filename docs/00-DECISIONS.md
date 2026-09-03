# 00 — Decisions and open questions

The interview log. Every product or process decision the owner has made lives here as a
**D-xx** entry; everything still unsettled is a **Q-xx**. Agents treat D-entries as
binding and Q-entries as "ask, do not guess". `/interview` appends to this file;
`/adr` cross-links technical decisions to `03-ARCHITECTURE.md`.

Owner: the Product Owner (writes no code). Interview round 1–4 held 2026-09-03 by the
orchestrator (Fable 5.1).

## Decisions

| ID   | Date       | Decision                                                                                                                                                                              | Consequence                                                                                        |
| ---- | ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| D-01 | 2026-09-03 | Product name **Savdo**. Repo `Sulton-Ali/savdo` (private).                                                                                                                             | Go module `github.com/Sulton-Ali/savdo`; brand strings in `packages/i18n`                          |
| D-02 | 2026-09-03 | **Single shop in MVP, designed for multi-tenant later.**                                                                                                                                | ADR-004: `shop_id` on every business table; exactly one `shops` row; no registration flow in MVP  |
| D-03 | 2026-09-03 | Business type: physical shop with a **catalog landing**; online checkout is **post-MVP**.                                                                                              | Landing has no cart/payment. Customers contact via Telegram/phone. Orders table deferred          |
| D-04 | 2026-09-03 | Market **Uzbekistan**: languages **uz, ru, en**; currency **UZS**.                                                                                                                      | i18n from day one; `NUMERIC(14,2)` money with currency on the shop row; phone format `+998`       |
| D-05 | 2026-09-03 | Sales entry: **manual quick sale in MVP; barcode scanning later.**                                                                                                                      | `product_variants.barcode` exists from day one; scanner UI is a backlog phase                     |
| D-06 | 2026-09-03 | Auth: **username + password**, plus **Telegram Login**; **OTP delivered via Telegram** for confirmations. No SMS.                                                                       | ADR-005. Telegram login/OTP ship with the bot phase; password login ships in Phase 1              |
| D-07 | 2026-09-03 | Hosting: **one VPS, Docker Compose, Caddy**.                                                                                                                                            | `07-DEVOPS.md`; no managed cloud services                                                         |
| D-08 | 2026-09-03 | MVP order: **Core API + admin web → mobile → landing → bot.**                                                                                                                           | `06-ROADMAP.md` phase order. API and admin web ship together per module (vertical slices)        |
| D-09 | 2026-09-03 | Bot AI model **undecided** between Claude Haiku 4.5, Gemini, and a self-hosted/cheap open-source model.                                                                                | ADR-009: provider-agnostic `internal/ai` adapter; see Q-01                                        |
| D-10 | 2026-09-03 | Landing on **TanStack Start** (owner prefers it over Next.js); admin on **Vite + React**.                                                                                               | Two web apps; shared generated API client in `packages/api-client`                                |
| D-11 | 2026-09-03 | Go stack: **stdlib router + pgx + sqlc + goose**, PostgreSQL. No ORM.                                                                                                                   | ADR-003                                                                                            |
| D-12 | 2026-09-03 | Mobile: **Expo, Android first**, APK/EAS delivery; iOS later.                                                                                                                           | No Apple account needed for MVP                                                                    |
| D-13 | 2026-09-03 | ERP scope in MVP: products with categories/images/prices; **incoming stock from suppliers**; **multiple locations**; **variants and units**. First client is a **clothing shop**.       | Variants (size/colour) are first-class; units support fractional quantities for later verticals   |
| D-14 | 2026-09-03 | CRM scope in MVP: **customer directory, suppliers directory, discounts/promotions.** Debt ledger (nasiya) is **post-MVP**.                                                              | `customers` has no balance column in MVP; ledger added later as its own module                    |
| D-15 | 2026-09-03 | Roles: **owner + staff with roles** (manager, cashier).                                                                                                                                 | ADR-010 permission matrix in `04-DATA-MODEL.md`                                                    |
| D-16 | 2026-09-03 | Fleet: orchestrator **Fable 5.1**; execution **Sonnet**; review **Sonnet + Opus**; reads/search/mechanical edits **Haiku**.                                                            | `.claude/agents/*`, `08-AI-WORKFLOW.md`                                                            |
| D-17 | 2026-09-03 | **Agents ask instead of guessing.** Orchestrator interviews the owner for missing details.                                                                                             | `/interview` skill; escalation rules in `AGENTS.md`                                                |
| D-18 | 2026-09-03 | All docs live under `docs/`; `CLAUDE.md`, `AGENTS.md`, skills and MCP config prepared **before** development starts.                                                                    | Phase 0 deliverable                                                                                |
| D-19 | 2026-09-03 | **No AI attribution trailers** on commits (`Co-authored-by`, `Claude-Session`, …).                                                                                                     | commit-msg hook in Phase 0                                                                         |
| D-20 | 2026-09-03 | Keep Karpathy's four guidelines in AGENTS.md verbatim, as a behaviour layer separate from the hard rules. | AGENTS.md § Karpathy guidelines; ADR-004/ADR-009 flexibility explicitly exempted |
| D-21 | 2026-09-03 | Vendor only 10 skills (+ linked checklists) from addyosmani/agent-skills, verbatim, pinned; no commands, hooks or agents from it. | .claude/skills/VENDORED.md; precedence rule in AGENTS.md § Vendored skills |

## Decisions made by the orchestrator (owner may overrule)

Routine technical calls taken during bootstrap so work can start. Each is an ADR in
`03-ARCHITECTURE.md`; overruling one is a doc change first.

| ID   | Decision                                                                                                | Why                                                                                                     |
| ---- | ------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------- |
| O-01 | Contract-first with OpenAPI 3.1 + oapi-codegen + openapi-typescript (ADR-002)                           | One source of truth for Go, web, admin and mobile; removes hand-written DTO drift                        |
| O-02 | Stock as an append-only movement ledger with a rebuildable `stock_levels` table (ADR-006)               | Auditable, safe under concurrency, standard for inventory                                                |
| O-03 | Media on local disk behind a `Storage` interface; no MinIO in MVP (ADR-008)                             | One VPS, one shop; S3 can be added without touching call sites                                          |
| O-04 | Opaque DB-backed sessions, not JWT (ADR-005)                                                            | Revocable, simple, no key rotation story needed for MVP                                                 |
| O-05 | Vertical slices: each catalog/stock/sales phase ships API **and** admin web screens together            | Owner can test each phase; matches D-08 (admin web first)                                                |
| O-06 | Bot and API are two binaries in one Go module sharing `internal/`                                       | Bot can be restarted/deployed alone; no duplicated domain code                                          |
| O-07 | Biome instead of ESLint + Prettier for TypeScript                                                        | One tool, fast, fewer configs for agents to get wrong                                                    |

## Open questions

Blocking questions are marked **[blocks Phase N]**. The orchestrator asks them via
`/interview` before that phase starts. Do not implement around an open question.

| ID   | Question                                                                                                                                                   | Blocks             | Options / notes                                                                                                                       |
| ---- | ---------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------- |
| Q-01 | Which LLM provider/model for the bot?                                                                                                                      | Phase 7            | Claude Haiku 4.5 ($1/$5 per MTok, tool use, easy); Gemini Flash (cheap, needs Google account); self-hosted OSS (no per-token cost, needs GPU/VPS RAM and ops). Adapter makes this swappable |
| Q-02 | Should cashier see stock quantities, or only "in stock / out of stock"?                                                                                    | Phase 3            | Draft matrix says quantities visible, cost price hidden                                                                               |
| Q-03 | Variant attributes for the clothing shop: fixed set (size, colour) or free-form per product?                                                               | Phase 2            | Draft: `attributes jsonb` with a per-shop attribute definition list (size, colour) so other verticals can add their own               |
| Q-04 | Price per product or per variant? Can a variant (e.g. XXL) cost more?                                                                                      | Phase 2            | Draft: product has a base price, variant may override                                                                                 |
| Q-05 | Which discounts in MVP: manual per-sale discount, per-product promo price with dates, or both?                                                             | Phase 4            | Draft: both; no coupon codes                                                                                                          |
| Q-06 | Receipt: printed? PDF/image shared via Telegram? None?                                                                                                     | Phase 4            | Draft: none in MVP, sale detail screen only                                                                                           |
| Q-07 | Sale payment methods: cash, card (terminal), transfer — any split payments?                                                                                | Phase 4            | Draft: one method per sale, enum cash/card/transfer                                                                                   |
| Q-08 | Landing content: which sections? (hero, categories, featured products, about, address/map, hours, contacts, social links)                                  | Phase 6            | Draft in `01-OVERVIEW.md` § Landing                                                                                                   |
| Q-09 | Domain name for the landing and admin (e.g. `savdo.uz`, `<shop>.savdo.uz`)?                                                                                | Phase 8            | Needed for Caddy, CORS and Telegram Login domain binding                                                                              |
| Q-10 | Reports needed in MVP: daily sales, sales by product, low stock, purchases by supplier — which are must-have?                                              | Phase 4            | Draft: daily/period sales totals, top products, low stock                                                                             |
| Q-11 | Who resets a forgotten password before Telegram OTP exists (Phase 7)?                                                                                      | Phase 1            | Draft: owner resets staff passwords from the admin; owner password reset via CLI command on the server                                |
| Q-12 | Product images: max count per product, and do variants have their own photos?                                                                              | Phase 2            | Draft: up to 8 per product, optional per-variant image                                                                                |
| Q-13 | Bot audience: customers only, or also a staff mode (e.g. "how many blue XL left?") behind Telegram login?                                                  | Phase 7            | Draft: customer mode in MVP; staff mode backlog                                                                                       |
| Q-14 | GitHub remote: `gh` CLI is not installed on this machine. Install it, or should the owner create the repo and give the URL?                                | Phase 0            | Orchestrator can create it once `gh auth login` is done                                                                               |
| Q-15 | Go toolchain is not installed locally. Install natively (recommended for agents' speed) or run everything in Docker?                                       | Phase 0            | Native install: `sudo pacman -S go` on CachyOS                                                                                        |

## Post-MVP backlog (agreed out of scope for now)

Debt ledger (nasiya) · barcode scanning · online cart + checkout with Payme/Click ·
multi-tenant registration and billing · iOS build · loyalty points · SMS OTP · offline
mobile mode · staff mode in the bot · printed receipts · accounting exports.
