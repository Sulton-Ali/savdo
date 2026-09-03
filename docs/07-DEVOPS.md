# 07 — DevOps

## Local development

Prerequisites on the developer machine (the agents' machine): Go (pin in
`02-TECH-STACK.md`), Node LTS via fnm, pnpm, Docker with Compose v2. `gh` CLI for the
GitHub remote. Currently missing here: Go and `gh` (Q-14, Q-15).

```bash
cp infra/.env.example infra/.env   # once; never commit .env
make dev-infra                     # Postgres on :5432
make migrate && make seed
make api                           # :8080
pnpm install
pnpm --filter admin dev            # :5173
pnpm --filter web dev              # :3000
pnpm --filter mobile start         # Expo dev server
```

Media files in dev go to `infra/data/media/` (gitignored).

## The gate: `make verify`

Runs, in order, and stops at the first failure:

1. `biome check` (format + lint, TS) and `gofmt -l` (must print nothing)
2. `golangci-lint run ./...`
3. `pnpm -r typecheck`
4. `make generate` then `git diff --exit-code -- api/gen packages/api-client api/internal/db` — generated code must be fresh
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

- `ci.yml`: on PR and push to `main` — `make verify` in a container with Docker service
  for testcontainers, plus Playwright e2e for `admin` and `web` against a compose stack.
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

Secrets: `infra/.env` on the VPS only, never in git; CI holds `SSH_HOST`, `SSH_USER`,
`SSH_KEY`, and the bot token/LLM key are set on the server.

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
