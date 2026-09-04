# 07 — DevOps

## Local development

Prerequisites on the developer machine (the agents' machine): Go, Node LTS via fnm, pnpm,
Docker with Compose, `gh`. Everything else is pinned and installed by the repo itself:
Go tools through the `go tool` directive in `api/go.mod`, golangci-lint by the Makefile,
TS tools as pnpm dev dependencies (D-24). Inspect the database with
`docker compose exec postgres psql -U savdo savdo` — there is no postgres MCP.

```bash
cp infra/.env.example infra/.env   # once; never commit .env (make dev-infra does it too)
make dev-infra                     # Postgres 18.6 on :5432
make migrate                       # goose via the savdo CLI (no migrations yet in Phase 0)
make api                           # Go API on :8080, serves /v1/*
pnpm install
pnpm --filter admin dev            # :5173 — Vite proxies /api/* → :8080 (prefix stripped), like Caddy in prod
pnpm --filter web dev              # :3000 — SSR; API_URL (server-side only) defaults to http://localhost:8080/v1
pnpm --filter mobile start         # Expo; EXPO_PUBLIC_API_URL (default http://10.0.2.2:8080/v1 for the emulator; LAN IP for a phone)
```

Seeded development accounts (`make seed`, D-30 — **dev only**, the seed refuses `ENV=prod`
without `--force`): shop `savdo-demo`; `owner` / `owner-dev-pass`, `manager` /
`manager-dev-pass`, `cashier` / `cashier-dev-pass`. Reset the owner password with
`printf 'new-password\n' | go run ./cmd/savdo reset-owner-password --password-stdin`
(from `api/`); it revokes the owner's sessions (D-28).

Media files in dev go to `infra/data/media/` (gitignored).

## Environment variables

The API and `savdo` CLI (including `seed` and `migrate` subcommands) load these from the
environment. Defaults shown are from `api/internal/config/config.go`. The API server
loads its config via `config.Load()` and enforces that, in **production, `MEDIA_DIR`
must be an absolute path**; the dev default is relative, and `config.Load()` fails fast
when `ENV=prod` and it isn't absolute (config.go L120-129).

| Variable            | Default        | Notes                                                                                 |
| ------------------- | -------------- | ------------------------------------------------------------------------------------- |
| `ENV`               | `dev`          | Set to `prod` on the VPS; gates `COOKIE_SECURE` and the seed guard                   |
| `API_ADDR`          | `:8080`        | TCP address the API binds on                                                          |
| `DATABASE_URL`      | (required)     | PostgreSQL connection string, e.g. `postgres://user:pass@host/dbname?sslmode=require` |
| `SHOP_SLUG`         | `savdo-demo`   | Single-shop MVP identifier; resolved to `shop_id` at startup (ADR-004)                |
| `SESSION_WEB_TTL`   | `168h` (7 d)   | Web cookie sliding window (D-29)                                                      |
| `SESSION_MOBILE_TTL`| `720h` (30 d)  | Mobile bearer token sliding window (D-29)                                             |
| `LOGIN_RATE_IP_PER_MIN` | `10`       | Per-IP login attempts per minute                                                      |
| `LOGIN_RATE_USER_PER_MIN` | `5`      | Per-username login attempts per minute                                                |
| `COOKIE_SECURE`     | dev: `false`, prod: `true` | Forces HTTPS-only session cookies; explicit env var overrides the default |
| `MEDIA_DIR`         | `../infra/data/media` | Absolute path in prod; local-disk root for media.LocalStorage (ADR-008). **In prod this is a Docker volume mounted at `/data/media`.** |
| `MEDIA_BASE_URL`    | `/media`       | URL prefix for all media.Storage keys returned to clients                             |
| `MEDIA_MAX_BYTES`   | `10485760`     | Single upload file part size cap (10 MB); checked before WebP encoding                |
| `MEDIA_CONCURRENCY` | `2`            | Max WebP derivative encode tasks running concurrently (gated by a semaphore; Review B) |
| `MEDIA_QUEUE`       | `8`            | Max uploads in flight (spooling + queued + encoding); admission gate outside encode queue |

## Shared local services during parallel work

All worktrees share ONE Compose project (`infra-postgres-1`) and ONE API port (8080).
Rules for parallel agents: never run `make dev-infra-down` while another agent may be
running; reviewers and mergers leave Postgres up and never kill a :8080 process they did
not start; verification dev servers use alternate ports (admin 5174, web 3001, Expo 8093).
The orchestrator sequences any task that needs exclusive use of the stack. At most one testcontainers-heavy gate (`make verify`, `go test ./...`) runs at a time — parallel gates produced test timeouts and a container-start deadline on 2026-09-04 (O-13).

## The gate: `make verify`

Runs, in order, and stops at the first failure:

1. `biome check` (format + lint, TS) and `gofmt -l` (must print nothing)
2. `golangci-lint run ./...`
3. `pnpm -r typecheck`
4. `make generate` then `git diff --exit-code -- api/gen api/internal/db packages/api-client/src/schema.d.ts` — generated code must be fresh. `web/src/routeTree.gen.ts` is regenerated by `pnpm --filter web build`; commit it exactly as produced
5. `go test ./...` (unit + testcontainers integration; Docker must be up)
6. `pnpm -r test` (Vitest)
7. `scripts/guards.sh` — greps that fail on: `float64` near money fields, `UPDATE
   stock_levels` outside `internal/stock`, hand-written response structs in handlers,
   `Co-authored-by` in the last commit message, `.env` tracked by git

Playwright e2e (`pnpm -r test:e2e`) runs in CI and in `/phase-done`, not in every
`verify`, because it needs the full stack up.

**The gate stays local.** The merging session runs it itself; a green CI run does not
replace it.

## Branch and merge protocol

How agent-authored work reaches `main`. This is the SDLC loop from
`08-AI-WORKFLOW.md` § The loop written as mechanics.

Until Phase 8 turns on required checks, "PR" means **a branch plus a review pass**; a
real GitHub PR is optional. The loop becomes a required PR + CI gate unchanged.

### The loop

| # | Who                                  | Does                                                                   |
| - | ------------------------------------ | ---------------------------------------------------------------------- |
| 1 | Orchestrator                         | Dispatches one task with its file scope                                |
| 2 | **Implementer** (or **db**)          | Creates the branch, writes code **and tests**, commits, runs `make verify` |
| 3 | **Reviewer** (fresh session)         | `/phase-review` against `git diff main...HEAD`. Findings only          |
| 4 | Implementer                          | Fixes findings on the same branch → back to 3                          |
| 5 | **Merger** (wrote none of the diff)  | Runs the merge checklist and `make verify`, merges `--no-ff`, deletes the branch |

The reviewer and the merger must be sessions that did not write the code. The owner
does not run this loop; they accept phases, not branches.

### Worktrees and parallelism

Parallel writers **never share one checkout**. Each concurrent implementer gets its own
`git worktree add .claude/worktrees/<task> -b <branch>` (gitignored). The merger uses
the primary checkout exclusively until its merges land. Reviewers read diffs without
checking out (`git diff main...<branch>`).

### Merge cadence and order

Merge early, in dependency order: contract → migration → service → handler → client.
Migration numbering: two branches that both add `NNNN_` with the same number are merged
serially; the second is renumbered **by its author after rebase**, and the merger
verifies `goose status` is clean.

### Review cadence

- Ordinary modules: one reviewer (Sonnet). APPROVE with only MINOR → merge; polish
  becomes a follow-up task.
- Correctness-critical (`auth`, `stock`, `sales`, `ai`/`bot` boundary): two reviewers on
  different models (Sonnet + Opus), split scopes; CRITICAL/MAJOR must close before merge.

### Branch naming

```
phase-<n>/t<task-id>-<short-slug>      phase-3/t4-stock-move-service
docs/<slug>                             doc-only changes, own branch, merged first
```

One branch, one task, one concern.

### Commits

Conventional Commits, enforced by a `lefthook` commit-msg hook. Scope = module
(`feat(stock): …`, `fix(admin): …`, `docs(api): …`, `chore(deps): …`, `merge(...)`).
**No attribution trailers** (D-19) — the hook rejects `Co-authored-by`,
`Claude-Session`, `Generated-with`, `Reviewed-by`. Body says **why**.

Doc corrections are their own commit on their own branch, merged before the code that
depends on them.

### Merging

`git merge --no-ff` by the merger. Never squash — the implement → review → fix history is
the process evidence. Subject: `merge(<scope>): <branch> — <one-line why>`. Body states
that the checklist below was checked and, for correctness-critical branches, that two
independent reviews on different models had no unresolved findings (as prose, not a
trailer).

### Merge checklist

- `make verify` green on the branch, run by the merger
- `/phase-review` reports no unresolved CRITICAL/MAJOR (and no MINOR for critical modules)
- Diff stayed inside the task's declared file scope
- Migration files are new (none edited), numbered after the latest on `main`
- `.github/pull_request_template.md` checklist passes
- No roadmap checkbox was ticked

## CI (GitHub Actions)

- `ci.yml`: on PR and push to `main` — `make dev-infra && make verify`, then `pnpm -r build` and the Expo Android export, on `ubuntu-latest` (Docker preinstalled). Playwright e2e is added in Phase 6.
- `deploy.yml` (Phase 8): on push to `main` — build images, `ssh` to the VPS, `docker
  compose pull && up -d` with migrations run by a one-shot container before `api` starts.
  A push to `main` after Phase 8 is a production deploy and an **owner decision**.

## Production (Phase 8)

One VPS (4 GB is enough for one shop). Compose services: `caddy`, `api`, `bot`,
`postgres`, `web` (TanStack Start node server). `admin` is a static build served by
Caddy at `/admin`. Media volume served by Caddy at `/media`. `/metrics` bound to the
internal network only.

Caddy: automatic TLS, security headers (HSTS, CSP for admin and web, `X-Frame-Options`),
gzip/zstd, `/api/*` → `api:8080`, `/media/*` → volume, `/admin/*` → static, `/*` →
`web:3000`. Domain per Q-09.

**Media on Caddy:** The `/media` location must include `@nosniff` header (`X-Content-Type-Options:
nosniff`) to prevent browser MIME sniffing on derivative image URLs (O-16). Media derivatives
are always WebP and served with content-type `image/webp`. **Storage sizing (estimate):** each
upload file is spooled to disk, then derivative encoding happens in a semaphore-gated slot.
`maxPixels` caps a decode at 24 megapixels, so the worst-case RGBA pixel buffer for one slot is
≈96 MB (`media/derive.go`), plus ≈10 MB for the spooled file — roughly 110 MB per slot. With the
default `MEDIA_CONCURRENCY=2`, that is roughly 220 MB worst case across both slots. `api` has
`http.Server.WriteTimeout 15 s`; a slow client downloading a large derivative could block a
write slot and eventually starve uploads if they saturate the queue.

Secrets: `infra/.env` on the VPS only, never in git; CI holds `SSH_HOST`, `SSH_USER`,
`SSH_KEY`, and the bot token/LLM key are set on the server.

Required environment in production: `ENV=prod` (turns on `COOKIE_SECURE` by default and
the seed guard), `SHOP_SLUG`, `DATABASE_URL`, `API_ADDR`, `MEDIA_DIR` (absolute path). Caddy
must declare `trusted_proxies` and forward the client address so the API can trust the **last**
`X-Forwarded-For` hop (the login rate limit and `sessions.ip` depend on it).

## Backups and restore

Nightly cron: `pg_dump -Fc` to `/opt/savdo/backups/` (14 days) + weekly copy off-box;
`rsync` of the media volume off-box. **A restore drill is a Phase 8 Done-when item and
counts only if a real dump was restored into a scratch container and the API booted
against it.**

## Runbook (grows with incidents)

- API down: `docker compose logs api --tail 200`; check `/readyz`; check Postgres.
- Stock numbers look wrong: `savdo stock rebuild --dry-run` shows the diff between
  movements and levels before applying.
- Bot silent: check webhook status via Telegram `getWebhookInfo`; check token budget in
  shop settings.
