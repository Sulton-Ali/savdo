# web

The public landing (TanStack Start, SSR) — see `docs/03-ARCHITECTURE.md` ADR-011 and
`docs/00-DECISIONS.md` D-100.

## Environment

Read server-side only (`web/src/lib/*.server.ts`), never exposed to the browser bundle:

- `API_URL` — base URL of the Go API's `/v1` prefix, e.g. `http://localhost:8080/v1`.
- `SITE_URL` — this site's own public URL, e.g. `http://localhost:3000` (D-100 placeholder
  until Q-09 picks a real domain in Phase 8; unused until T5b's sitemap/canonical/OG work).
