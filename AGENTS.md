# AGENTS.md

Source of truth for how AI agents operate in this repo. `CLAUDE.md` points here; if
they conflict, **this file wins**.

## What this is

**Savdo** ("trade" in Uzbek): a light ERP + CRM for small shops in Uzbekistan. One Go
API serves a public landing (TanStack Start, SSR), an admin panel (Vite + React), a
mobile admin app (Expo, Android first) and a Telegram bot that answers customers with an
LLM grounded in the shop's own data. uz/ru/en, UZS. Built single-shop, tenant-ready
(ADR-004). First client: a family clothing shop.

## Operating mode — read this first

**AI-native: agents write all code; the owner writes none.** The owner (Product Owner)
sets scope, answers questions, approves architecture, dependencies and money, and
accepts or rejects a closed phase.

- **The docs are the specification.** `docs/` is normative. Code that disagrees with a
  doc is a bug in the code; if the doc is wrong, the owner rules and the doc is fixed
  first, in its own commit.
- **Ask, don't guess.** Ambiguity goes to the owner via the orchestrator. A lucky guess
  is still a process failure.
- **Stay inside your task's file scope.** Needing more means the task was cut wrong:
  stop and say so. No exploring, no "also fixing".
- **One task, one branch, one concern. Tests ship with the code, by its author.**
- **Never tick a roadmap box** — only `/phase-done` does, after running the Done-when bar.
- **Reviewers report, never fix. No agent merges its own work.**
- **Verify versions and APIs against the registry or context7**, never training data.

Talk to the owner in plain English (CEFR B1/B2). Docs, code and comments in normal
technical English.

## Karpathy guidelines

From [Karpathy's observations](https://x.com/karpathy/status/2015883857489522876) via the
MIT [karpathy-guidelines](https://github.com/multica-ai/andrej-karpathy-skills) skill.
Caution over speed; use judgement on trivial tasks. They never override `docs/` or the
hard rules.

1. **Think before coding.** State assumptions; surface interpretations and trade-offs
   instead of picking silently; if unclear, stop and ask.
2. **Simplicity first.** Minimum code that solves the ask; nothing speculative, no
   single-use abstractions, no unrequested configurability. (ADR-004 tenant-readiness
   and the ADR-009 LLM adapter are owner-requested: D-02, D-09.)
3. **Surgical changes.** Touch only what the task needs; match existing style; clean up
   only orphans your change made; mention other dead code, don't delete it.
4. **Goal-driven execution.** Turn the ask into a verifiable check (test, command,
   behaviour); plan as step → verify; loop until it passes.

## Docs map

`00-DECISIONS` owner decisions D-xx and open questions Q-xx
· `01-OVERVIEW` pitch, personas, non-goals · `02-TECH-STACK` pinned versions
· `03-ARCHITECTURE` layout, flows, ADR-001…014 · `04-DATA-MODEL` schema, ledgers,
permission matrix, rules · `05-API` conventions and endpoint catalogue
· `06-ROADMAP` **living state: first phase with unchecked boxes is current**
· `07-DEVOPS` local stack, `make verify`, **branch/merge protocol** (read before any
branch) · `08-AI-WORKFLOW` fleet, loop, context discipline, failure modes.

## Stack (orientation only — `02-TECH-STACK.md` has the pins)

Go stdlib `net/http` · pgx · sqlc · goose SQL migrations · PostgreSQL (`NUMERIC` money,
`uuid` v7, `timestamptz`) · OpenAPI 3.1 in `contracts/openapi.yaml` → oapi-codegen +
openapi-typescript/openapi-fetch · media on a disk volume behind a `Storage` interface ·
TanStack Start (`web/`) · Vite + React + TanStack Router/Query + shadcn (`admin/`) · Expo
+ Expo Router + NativeWind (`mobile/`) · `go-telegram/bot` + `internal/ai` adapter
(`api/cmd/bot`) · i18next with JSON in `packages/i18n` · one VPS, Docker Compose, Caddy.

## Conventions

- Layout: `api/`, `contracts/`, `web/`, `admin/`, `mobile/`, `packages/`, `infra/`,
  `docs/`. Root `Makefile` runs every check. **pnpm for TypeScript, never npm/yarn.**
- Go: `api/cmd/<binary>`, `api/internal/<module>` (`auth shop catalog media stock sales
  crm content reports ai bot httpx db`). Handler → service → sqlc; **no ORM**.
  Migrations `api/db/migrations/NNNN_<slug>.sql`, one concern, additive, never edited
  after merge.
- **Contract first** (ADR-002): change `contracts/openapi.yaml` → `make generate` → Go
  handler → client. No hand-declared request/response shapes anywhere. Use `/api-change`.
- TypeScript strict + `noUncheckedIndexedAccess`; Biome. DB `snake_case`, Go
  `CamelCase`, JSON `camelCase` — generators map, nobody hand-writes mappings.
- Errors return a machine-readable `code`, never a display sentence (ADR-013).
- Conventional Commits, scope = module. **No attribution trailers** (`Co-authored-by`,
  `Claude-Session`, …) — owner decision D-19; the commit-msg hook rejects them.
- Tests: Go `testing` + testcontainers Postgres; Vitest; Playwright.

## Hard rules — violating these fails review

1. Every business-table query filters by `shop_id` from the auth context, never from
   the request (ADR-004).
2. Never `UPDATE stock_levels` directly; stock changes are `stock_movements` written by
   `stock.Service.Move` in one transaction; levels are rebuildable from movements (ADR-006).
3. Never mutate a completed sale; corrections are a void or a return with their own
   movements (ADR-014).
4. Money is `NUMERIC(14,2)` / decimal, never float; currency from the shop row (ADR-007).
5. `cost_price`, `unit_cost` and margins never reach a cashier, public or bot response;
   filtered in the service, not the UI (ADR-010).
6. No hand-declared request/response types; shapes come from the contract (ADR-002).
7. Never edit a merged migration; destructive changes need owner approval quoted in
   the task and the merge body.
8. Never trust client totals or quantities; compute server-side.
9. No secret, token, password or key in logs, docs, fixtures or commits; passwords
   argon2id, session tokens stored hashed (ADR-005).
10. The bot's customer mode answers only through its public-read tools (products,
    prices, availability, hours, contacts); it never sees cost, quantities, customers,
    staff or sales (ADR-009).
11. No new dependency or version bump without owner approval, checked in the registry.
12. No LLM call outside the bot handler, and never without per-chat and per-shop
    limits (ADR-009).

## Vendored skills

Ten general skills from [addyosmani/agent-skills](https://github.com/addyosmani/agent-skills)
live verbatim in `.claude/skills/` (pinned in `.claude/skills/VENDORED.md`). Reference
knowledge, not process: **this file and the Savdo skills win on conflict** (branches
merged `--no-ff` by a separate session, never trunk-based; review and merge never
collapse into one session). Each agent definition names the skills it may load; the
scribe loads none.

## Escalate to the owner

Non-goals (`01-OVERVIEW.md`) and open Q-xx · new dependencies or bumps · new or reversed
ADRs · non-additive or destructive schema changes · auth, roles, rate limits, CORS, CSP ·
money math, ledger integrity, sale immutability · LLM provider/model · anything costing
money or reaching a real user.

## Workflow

Roadmap-phase-driven (`06-ROADMAP.md`); work outside the current phase needs approval.
A phase closes only when its Done-when bar holds end to end, ticked by `/phase-done`.

**Fleet** (`08-AI-WORKFLOW.md`): the interactive session is the orchestrator on
**Fable 5.1** — it interviews, decomposes, dispatches with minimum context, tracks; it
does not execute. Scribe **Haiku** (reads, search, mechanical edits) · implementer, db,
merger **Sonnet** · reviewer **Sonnet**, plus **Opus** in a second fresh session for
correctness-critical work: `auth`, `stock`, `sales`, the `ai`/`bot` data boundary.

**Loop**: `/interview` → `/phase-start` → implement on `phase-<n>/t<id>-<slug>` →
`/phase-review` (fresh session) → fix → merger (third session) runs **`make verify`**
and merges `--no-ff` → `/phase-done`. Mechanics in `07-DEVOPS.md` § Branch and merge
protocol; parallel-agent constraints in `08-AI-WORKFLOW.md`. The gate is local; a green
CI run is not a substitute.

## Commands (Phase 0 delivers these)

```bash
make verify        # THE GATE: format → lint → typecheck → generated-code-fresh → tests
make generate      # oapi-codegen + sqlc + openapi-typescript; commit the output
make dev-infra | dev-infra-down | migrate | seed | api | bot
pnpm --filter admin dev · pnpm --filter web dev · pnpm --filter mobile start
```

## MCP

`context7` (library docs — mandatory before asserting an API) · `postgres` (read-only
inspection of the Compose DB, from Phase 1) · `playwright` (browser verification for
`/phase-done`, from Phase 2).
