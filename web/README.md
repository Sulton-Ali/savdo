# web

The public landing (TanStack Start, SSR) — see `docs/03-ARCHITECTURE.md` ADR-011 and
`docs/00-DECISIONS.md` D-100.

## Environment

Read server-side only (`web/src/lib/*.server.ts`), never exposed to the browser bundle:

- `API_URL` — base URL of the Go API's `/v1` prefix, e.g. `http://localhost:8080/v1`.
- `SITE_URL` — this site's own public URL, e.g. `http://localhost:3000` (D-100 placeholder
  until Q-09 picks a real domain in Phase 8; used by T5b's sitemap/canonical/OG work).

## Dev-only media proxy

`MediaUrls` (product/hero images) are site-relative paths (`/media/<shop>/<yyyy>/<mm>/<id>_<size>.webp`,
ADR-008) — in production Caddy serves the API's `/media/*` on the same host as the landing
(`docs/07-DEVOPS.md` § Production), so a relative `<img src>` just works. `vite.config.ts`
proxies `/media` to `http://localhost:8080` in dev so the same relative URLs resolve locally
without making them absolute in app code.

## e2e (Playwright)

`e2e/` holds Chromium-only Playwright specs against a production build (`playwright.config.ts`
builds and serves it on `:3100` via `vite preview`). Needs the Go API up on `:8080` with
the seeded demo shop (`make dev-infra && make migrate && make seed && make api` from the
repo root, in another terminal — not started by this config).

```bash
pnpm exec playwright install chromium   # once per machine, if not already cached
pnpm --filter web test:e2e              # or: make test-e2e (repo root)
```

Not part of `make verify` — see `docs/07-DEVOPS.md` § The gate.

## Lighthouse

`pnpm --filter web lighthouse` builds, serves the same production build on `:3100`, and
audits `/uz` and one product page with the Chromium binary already installed for
Playwright (no separate Chrome download). Fails (exit 1) if performance or SEO drops
below 90 on either page; HTML reports land in `web/.lighthouse/` (gitignored). Also
needs the API up on `:8080` with the seeded demo shop.
