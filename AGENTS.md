# AGENTS.md

Source of truth for how AI agents operate in this repo. Tool-specific files
(`CLAUDE.md`) point back here. If they conflict, **this file wins**.

## What this is

**Savdo** ("trade" in Uzbek) is a light ERP + CRM for small shops in Uzbekistan. One Go
API serves four clients: a public landing (TanStack Start, SSR for SEO), an admin panel
(Vite + React SPA), a mobile admin app (Expo / React Native, Android first) and a Telegram
bot that answers customer questions with AI grounded in the shop's own data.

First client: a family clothing shop — sizes, colours, seasonal stock. Trilingual
(uz/ru/en), currency UZS. Built as a **single shop**, designed **tenant-ready** (every
business table carries `shop_id`; see ADR-004).

## Operating mode — READ THIS FIRST

**This project is AI-native. You write the code. The owner writes none.**

The owner is the Product Owner: sets scope, answers questions, approves architecture,
dependencies and money, and accepts or rejects a closed phase. Everything else —
implementation, tests, review, merges, docs — is done by agents.

"You write the code" is not "you decide the product". These constraints make delegation
safe:

- **The docs are the specification.** `docs/` is normative. Code that disagrees with a
  doc is a bug in the code — unless the owner rules the doc wrong, in which case the doc
  is fixed first, in its own commit.
- **Ask, don't guess.** An ambiguity in a spec goes back to the owner through the
  orchestrator. A guess that turns out right is still a process failure, because the
  next one will not be. See § Escalate to the owner.
- **Stay inside your task's file scope.** If a task names the files you may touch, that
  is the boundary. Needing something outside it means the decomposition was wrong: stop
  and say so. Do not go exploring and do not "also fix" adjacent things.
- **One task, one branch, one concern.** An unrelated improvement in the same change
  gets the whole change sent back, however good it is.
- **Tests ship with the code**, in the same change, written by whoever wrote the code.
- **Never tick a roadmap box.** Only `/phase-done` does that, after verifying the
  phase's Done-when bar by actually running it.
- **Reviewers report; they never fix.** A reviewer that edits has reviewed itself.
- **No agent merges its own work.** Agents do merge — the owner delegated that — but the
  session that wrote the diff is never the session that merges it.
- **Verify versions and API surfaces against the registry or context7**, never against
  training data. The stack is current as of September 2026 and training data lags it.

**Plain English with the owner.** Keep replies at CEFR B1/B2: short sentences, common
words, explain jargon the first time it appears. Docs, code and comments stay in normal
technical English.

## Karpathy guidelines (compact)

Behavioural defaults for LLM coding. Bias: caution over speed; for trivial tasks, use
judgement. They reinforce the operating mode — they never override `docs/` or the hard
rules below.

1. **Think before coding.** State assumptions; if unsure, ask. Surface multiple
   interpretations and trade-offs — do not pick silently. Name confusion and stop.
2. **Simplicity first.** Minimum code that solves the ask. No speculative features,
   single-use abstractions or unrequested flexibility. If it could be a third of the
   size, rewrite it.
3. **Surgical changes.** Touch only what the task requires. Match existing style. Clean
   up only orphans your change created; mention other dead code, do not delete it.
4. **Goal-driven execution.** Turn the ask into a checkable outcome (a test, a command,
   a Done-when behaviour). Plan multi-step work as step → verify. Loop until the check
   passes — "make it work" is not a goal.

## Docs map

| Doc                       | Contents                                                            | Read it before...                                  |
| ------------------------- | ------------------------------------------------------------------- | -------------------------------------------------- |
| `docs/00-DECISIONS.md`    | Owner decisions (D-xx) and open questions (Q-xx) from the interview | Assuming anything about scope or product behaviour |
| `docs/01-OVERVIEW.md`     | Pitch, personas, user stories, non-goals, definition of done        | Touching product scope or feature intent           |
| `docs/02-TECH-STACK.md`   | Pinned versions and rationale                                       | Adding, bumping or importing any dependency        |
| `docs/03-ARCHITECTURE.md` | Monorepo layout, module map, key flows, **ADR-001…014**             | Designing anything cross-cutting                   |
| `docs/04-DATA-MODEL.md`   | Schema, ledgers, permission matrix, rules for agents                | Any DB or migration work                           |
| `docs/05-API.md`          | REST conventions, errors, pagination, auth, endpoint catalogue      | Adding or changing an endpoint                     |
| `docs/06-ROADMAP.md`      | **Living state** — the first phase with unchecked boxes is current  | Anything at all                                    |
| `docs/07-DEVOPS.md`       | Local stack, the `make verify` gate, **branch/merge protocol**, deploy | Infra work, and **before creating any branch**  |
| `docs/08-AI-WORKFLOW.md`  | The fleet, the loop, context discipline, failure modes, metrics     | Orchestrating or dispatching other agents          |

## Stack (condensed)

`docs/02-TECH-STACK.md` is the authority with exact pins. This is orientation only.

| Layer    | Choice                                                                                       |
| -------- | -------------------------------------------------------------------------------------------- |
| API      | Go (stdlib `net/http` mux, no framework) · pgx · **sqlc** · **goose** SQL migrations · slog  |
| Contract | **OpenAPI 3.1** in `contracts/openapi.yaml` → oapi-codegen (Go) + openapi-typescript (TS)     |
| Database | PostgreSQL. **System of record.** Money as `NUMERIC(14,2)`, ids `uuid`, time `timestamptz`   |
| Media    | Local disk volume served by Caddy, behind a Go `Storage` interface (ADR-008). No MinIO in MVP |
| Landing  | **TanStack Start** (SSR) + React + Tailwind — `web/`                                         |
| Admin    | **Vite + React** SPA, TanStack Router + Query, shadcn/ui, Tailwind — `admin/`                |
| Mobile   | **Expo** (Expo Router, NativeWind), Android first — `mobile/`                                |
| Bot      | Go binary `api/cmd/bot`, `go-telegram/bot`, LLM behind `internal/ai` adapter (ADR-009)       |
| i18n     | Shared JSON in `packages/i18n`; product/category translations in DB                          |
| Tooling  | pnpm workspaces · Biome · Vitest · Playwright · golangci-lint · testcontainers-go · Make      |
| Hosting  | One VPS, Docker Compose, Caddy (TLS). Static admin build served by Caddy                     |

## Conventions

- **pnpm is the package manager for TypeScript — never npm, never yarn.** Go uses modules.
- Monorepo: `api/`, `contracts/`, `web/`, `admin/`, `mobile/`, `packages/`, `infra/`,
  `docs/`. Root `Makefile` is the entry point for every check.
- Go: standard layout `api/cmd/<binary>`, `api/internal/<module>`. One module per
  business area (`auth`, `shop`, `catalog`, `stock`, `sales`, `crm`, `content`, `bot`,
  `ai`, `media`). Handlers implement the oapi-codegen interface; business rules live in
  a service; SQL lives in `api/db/queries/*.sql` compiled by sqlc. **No ORM.**
- Migrations: `api/db/migrations/NNNN_<slug>.sql`, goose format, one concern each,
  additive by default. Never edit a migration that has merged.
- TypeScript strict everywhere, `noUncheckedIndexedAccess` on. Biome for lint + format.
- Database `snake_case`; Go `CamelCase`; JSON/API `camelCase`. sqlc and oapi-codegen do
  the mapping; nobody hand-writes a mapping struct.
- **Contract first.** A new or changed endpoint starts in `contracts/openapi.yaml`, then
  `make generate`, then the Go handler, then the client. Request/response shapes are
  never declared by hand on either side (ADR-002). Use `/api-change`.
- API errors return a machine-readable `code`. **Never a human sentence for display** —
  clients translate codes (`docs/05-API.md` § Errors).
- Conventional Commits. Scope = module name (`feat(stock): …`). **Never** add
  `Co-authored-by`, `Claude-Session`, `Generated-with` or any agent/tool attribution
  trailer to commits or merges (owner decision D-19). The commit-msg hook rejects them.
- Tests: Go `testing` + testcontainers-go against real Postgres (integration), Vitest
  (unit, web), Playwright (e2e, admin + landing).

## Hard rules — violating these fails review

1. **Every query on a business table filters by `shop_id`** taken from the auth
   context, never from the request body or query string (ADR-004).
2. **Never `UPDATE stock_levels` directly.** Stock changes are `stock_movements` rows
   inserted through the stock service, which updates `stock_levels` in the same
   transaction (ADR-006). `stock_levels` is rebuildable from movements.
3. **Never mutate a completed sale.** Corrections are a `void` or `return`, each of
   which writes its own stock movements (ADR-014).
4. **Money is `NUMERIC(14,2)`, never float**, in Go `decimal`-typed or `pgtype.Numeric`,
   never `float64`. Currency comes from the shop row.
5. **Cost price and margin never reach a `cashier` role response or any public/bot
   customer response.** Field-level filtering is enforced in the service, not the UI
   (ADR-010).
6. **Never hand-declare a request or response struct/type.** Shapes come from
   `contracts/openapi.yaml` through the generators (ADR-002).
7. **Never edit a merged migration.** Fix forward with a new one. Destructive changes
   (`DROP`, type narrowing, removing an enum value) need explicit owner approval in the
   task and the merge commit body.
8. **Never trust client-supplied totals.** Sale totals, discounts and stock quantities
   are computed server-side from line items and current prices.
9. **Never store a session secret, password, bot token or API key in a log, a doc, a
   test fixture or a commit.** Passwords are argon2id hashes; session tokens are stored
   hashed (ADR-005).
10. **The bot's customer mode answers only through its tools** (public products, prices,
    availability yes/no, hours, address, contacts). It never sees cost price, stock
    quantities, customers, staff or sales (ADR-009). A tool that would expose those is a
    CRITICAL finding.
11. **Never add a dependency or bump a pinned version** without owner approval, and
    never from memory — check the registry.
12. **Never call an LLM from a request path other than the bot's own handler**, and
    never without the per-shop and per-user rate limit in place (ADR-009).

## Escalate to the owner

Stop and ask (through the orchestrator) rather than deciding, for:

- Anything in `docs/01-OVERVIEW.md` § Non-goals, or any open question in
  `docs/00-DECISIONS.md`
- New dependencies or version bumps
- New or reversed ADRs
- Non-additive schema changes; anything destructive
- Auth, authorization, roles, rate limits, CORS, CSP
- Anything touching money math, stock ledger integrity or sale immutability
- The LLM provider/model choice, and anything that costs money or reaches a real user

## Workflow

Development is roadmap-phase-driven (`docs/06-ROADMAP.md`). **The first phase with
unchecked boxes is the current phase.** Work outside it needs owner approval.

A phase closes only when its **Done when** bar holds end to end — not when its tasks are
individually checked. Boxes are ticked exclusively by `/phase-done`.

**Correctness-critical modules** — `auth` (sessions, roles, permissions), `stock`
(ledger), `sales` (money, immutability, stock decrement) and the `ai`/`bot` customer-mode
data boundary — require **two reviewers on different models** (Sonnet and Opus), each a
fresh session with a split scope.

**Harness models** (`docs/08-AI-WORKFLOW.md` § The fleet): the interactive session IS the
orchestrator, on **Fable 5.1**. It reasons, interviews the owner, decomposes, dispatches
with minimum context, tracks the roadmap. **It does not execute.** Reads, searches and
mechanical edits → **Haiku** (scribe). Implementation, DB work and delegated merges →
**Sonnet**. Review → **Sonnet**; correctness-critical adds **Opus**.

**Branch and merge mechanics — `docs/07-DEVOPS.md` § Branch and merge protocol.** Read it
before creating a branch. Operating constraints for parallel agents:
`docs/08-AI-WORKFLOW.md` § Operating constraints.

**The gate is `make verify`** (format → lint → typecheck → generated-code-fresh → tests),
run locally before every merge by the merging session. A green remote run is not a
substitute.

## Commands

Phase 0 creates these. Until it lands, the targets are the specification of what Phase 0
must deliver.

```bash
make verify            # THE GATE
make generate          # oapi-codegen + sqlc + openapi-typescript; commit the output
make dev-infra         # docker compose up -d (Postgres, Mailpit if needed)
make dev-infra-down
make migrate           # goose up against the compose database
make seed              # demo shop, users, clothing catalogue
make api               # go run ./cmd/api on :8080
make bot               # go run ./cmd/bot (long polling)
pnpm --filter admin dev
pnpm --filter web dev
pnpm --filter mobile start
```

Current roadmap phase: **Phase 0 — Bootstrap** as of 2026-09-03. Docs and agent
configuration exist; no application code yet.

## MCP

- **context7** — current library docs. Verify API surfaces before asserting how
  something works, and especially before "fixing" code that merely looks unfamiliar.
- **postgres** — read-only inspection of the local Compose database; enabled once
  Phase 0 brings the database up. Schema changes still go through goose migrations.
- **playwright** — drives a real browser for `/phase-done` verification of the admin
  panel and the landing. Enabled from Phase 2.
