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

`make seed` also seeds a full demo clothing catalogue for `savdo-demo` (Phase 2 T5): 4
units, size/colour attribute definitions, ~8 categories (Men/Women/Kids), ~30 products
with variants and generated placeholder images, entirely through the catalog service and
media pipeline — idempotent, so re-running it never duplicates anything. This step needs
`MEDIA_DIR` set (`infra/.env` already sets it for `make seed`; running `go run ./cmd/savdo
seed` directly needs `DATABASE_URL` and `MEDIA_DIR` in the environment, same as `cmd/api`).

Media files in dev go to `infra/data/media/` (gitignored).

Phase 3 (D-49): `make seed` then creates three suppliers and opening stock through the services, never by writing `stock_levels` or `stock_movements` directly (ADR-006): one received purchase per catalogue category (6 purchases, one line per variant, 135 lines for the demo catalogue), quantities 3–12 derived from a stable hash of the SKU, `unitCost` equal to the variant's effective cost so receiving leaves the catalogue's cost numbers unchanged (D-42), then one transfer of 2 units for the first 10 variants by SKU from the shop floor to the storeroom. The step is skipped when the shop already has a purchase (`stock already seeded`). The seed is additive-only and has no destructive reset; a full local reset is `make dev-infra-down && make dev-infra && make migrate && make seed`, which recreates the database (the ledger's append-only trigger blocks per-shop deletes by design).

### Stock rebuild

`cd api && go run ./cmd/savdo stock rebuild --shop-slug savdo-demo` recomputes `stock_levels` from `stock_movements` for one shop inside a single transaction under a shop-scoped advisory lock (ADR-006). The flag is required. Stop the API (or make sure nothing writes stock) while it runs: a concurrent movement blocks on the rebuild or makes it abort with a unique violation; levels are never silently overwritten. Prints movement and level counts.

## Android build and install (Phase 5)

D-72: the release APK is built locally with Expo prebuild + Gradle, no EAS cloud, no Expo
account. Signed with the Gradle/Android default **debug keystore** for now — this is not
a store-distributable artifact, only a local install for testing on the owner's phone or
an emulator.

### Prerequisites (dev machine only, not in `make verify`)

- Android SDK at `$ANDROID_HOME` (e.g. `~/Android/Sdk`) with: command-line tools,
  `platform-tools`, `platforms;android-36`, `build-tools;36.0.0`, `ndk;27.1.12297006`,
  `cmake;3.22.1`, `emulator`, a system image if you want an emulator
  (`system-images;android-36;google_apis;x86_64`). These match React Native 0.86.3's pins
  (compileSdk 36, buildTools 36.0.0, NDK 27.1.12297006, minSdk 24, AGP 8.12).
- JDK 21 (Temurin). `JAVA_HOME` must point at it.
- Environment (add to your shell profile):
  ```bash
  export ANDROID_HOME="$HOME/Android/Sdk"
  export ANDROID_SDK_ROOT="$ANDROID_HOME"
  export PATH="$PATH:$ANDROID_HOME/cmdline-tools/latest/bin:$ANDROID_HOME/platform-tools:$ANDROID_HOME/emulator"
  export JAVA_HOME="$HOME/.sdkman/candidates/java/current"   # or wherever your JDK 21 lives
  ```
- No Android Studio needed. The Gradle wrapper downloads Gradle itself on first run
  (needs network once).

### Build commands

```bash
pnpm --filter mobile android:release       # expo prebuild --platform android (regenerates mobile/android/, gitignored)
                                            # then ./gradlew assembleRelease inside it
```

This is a `prebuild` + `gradlew assembleRelease` pair rather than `expo run:android
--variant release`, because `run:android` requires a connected device or emulator to pick
a target; `assembleRelease` alone produces the APK without one. If a device is already
connected/booted, `pnpm --filter mobile android:run-release` (`expo run:android --variant
release`) also works and additionally installs and launches it.

The APK lands at `mobile/android/app/build/outputs/apk/release/app-release.apk`. Neither
`mobile/android/` (regenerated every prebuild, gitignored) nor the APK is committed.

**Fixed issue (found and fixed 2026-09-05, D-86):** the Gradle-invoked JS bundling step
(`:app:createBundleReleaseJsAndAssets`, which runs the Expo CLI's internal `export:embed`
command) reproducibly failed in this pnpm workspace with `Cannot find module
'@babel/plugin-transform-react-jsx'`, while the equivalent `pnpm --filter mobile
export:android` (`expo export --platform android`, our `make verify` and CI check)
reliably succeeded using the exact same babel config. Root cause: `nativewind/babel` (via
`react-native-css-interop@0.2.6`'s `babel.js`) declares `"@babel/plugin-transform-react-jsx"`
as a plugin **by string name** without listing it as `react-native-css-interop`'s own
dependency; under pnpm's strict, non-hoisted `node_modules`, Babel's string-based plugin
resolution can only find it once it's hoisted into a package that resolves in that lookup
chain, and the CLI's `export` and `export:embed` commands hit this resolution in a way that
differs in this workspace layout. Fixed by declaring `@babel/plugin-transform-react-jsx`
(pinned `7.29.7`, the exact version already resolved transitively via `babel-preset-expo`
— no new supply-chain surface) as an explicit `mobile` devDependency, which makes pnpm
symlink it directly into `mobile/node_modules/@babel/`.

### Install on a phone over USB (owner's workflow)

```bash
adb devices                 # phone must show up as "device", not "unauthorized"
adb install -r mobile/android/app/build/outputs/apk/release/app-release.apk
```

On Arch Linux, USB device access needs the udev rules and group membership (owner runs
this once): `sudo pacman -S android-udev && sudo usermod -aG adbusers $USER`, then
re-plug the phone and accept the "Allow USB debugging" prompt on it.

### Reaching the dev API from the phone

The app's login screen has an editable server URL (D-79), prefilled from
`EXPO_PUBLIC_API_URL` at build time and remembered on the device; plain `http://` is only
accepted for loopback/private hosts, cleartext is enabled for this in the APK (D-81/D-82).

- **Cable-first (recommended):** with the phone plugged in and the API running on the dev
  machine (`make api`, `:8080`), run `adb reverse tcp:8080 tcp:8080` so the phone's
  `127.0.0.1:8080` reaches the dev machine's API. Set the server URL on the login screen to
  `http://127.0.0.1:8080/v1`.
- **Wi-Fi:** the API listens on all interfaces, so `http://<dev-machine-LAN-IP>:8080/v1`
  also works as long as the phone and dev machine are on the same network.
- Either way, product images need `MEDIA_DIR` to resolve from the same API instance the
  phone talks to — see § Shared local services above if you're running multiple worktrees.

### Emulator alternative

```bash
emulator -avd savdo36 -memory 1536
```

then install the same APK with `adb install -r ...`. On the emulator, the server URL is
`http://10.0.2.2:8080/v1` (the emulator's alias for the host's `localhost`). **Only one
emulator instance at a time** on the dev machine — it's heavy (CPU/RAM) and multiple
agents/tasks share the same machine; coordinate before booting one.

## Environment variables

The API loads all of these via `config.Load()`. `savdo migrate` reads only
`DATABASE_URL`; `savdo seed` loads the full config via `config.Load()` (it needs
`MEDIA_DIR` and `MEDIA_BASE_URL` for the catalogue images). Defaults shown are from
`api/internal/config/config.go`. `config.Load()`
also enforces that, in **production, `MEDIA_DIR` must be an absolute path**; the dev
default is relative, and `config.Load()` fails fast when `ENV=prod` and it isn't
absolute (config.go L120-129).

| Variable            | Default        | Notes                                                                                 |
| ------------------- | -------------- | ------------------------------------------------------------------------------------- |
| `ENV`               | `dev`          | Set to `prod` on the VPS; gates `COOKIE_SECURE` and the seed guard                   |
| `API_ADDR`          | `:8080`        | TCP address the API binds on                                                          |
| `DATABASE_URL`      | (required)     | PostgreSQL connection string, e.g. `postgres://user:pass@host/dbname?sslmode=require` |
| `SHOP_SLUG`         | `savdo-demo`   | Single-shop MVP identifier; resolved to `shop_id` at startup (ADR-004)                |
| `PUBLIC_SHOP_SLUG`  | `savdo-demo`   | Public landing shop slug (Phase 6, D-105); Phase 8 resolves by hostname               |
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

`MEDIA_DIR` defaults to `../infra/data/media` relative to the API process cwd (`api/internal/config/config.go`), so every git worktree has its own media root; a seed run in one worktree and an API started from another see different, independently empty folders and media URLs 404. When running the API for a phone/emulator smoke from a worktree, either seed from that same worktree or set `MEDIA_DIR` to the absolute path of the primary checkout's `infra/data/media`.

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

Playwright e2e (`pnpm -r test:e2e`, or `make test-e2e`) runs in CI and in `/phase-done`,
not in every `verify`, because it needs the full stack up: Postgres, the API on `:8080`
with the seeded demo shop, and `API_URL`/`SITE_URL` for `web`'s own `webServer` build.

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

- `ci.yml`: on PR and push to `main` — `make dev-infra && make verify`, then `pnpm -r build` and `pnpm --filter mobile export:android` (the Expo Android JS export only — no Gradle, no Android SDK; the Gradle release APK build is local-only per D-72), on `ubuntu-latest` (Docker preinstalled). Playwright e2e is added in Phase 6.
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
