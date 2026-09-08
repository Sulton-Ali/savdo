import { defineConfig, devices } from "@playwright/test";

const PORT = 3100;
const BASE_URL = `http://localhost:${PORT}`;

/**
 * Chromium-only e2e against a production build (`vite build` then
 * `vite preview`, which serves the real SSR HTML — verified by hand: the
 * response to `GET /uz` already carries the rendered `<title>`/meta tags,
 * unlike the dev server which is also SSR but is not the artifact the
 * Done-when bar and Lighthouse both target). `webServer` builds and starts
 * the preview server itself; set `reuseExistingServer` so a server already
 * running on :3100 (e.g. started by hand while iterating on a test) is
 * reused instead of rebuilt. Needs the Go API up on `:8080` with the seeded
 * demo shop (`docs/07-DEVOPS.md` § The gate) — not started here.
 *
 * The bundled chromium build this config needs is already cached under
 * `~/.cache/ms-playwright` on this machine; run
 * `pnpm exec playwright install chromium` once per machine if it is
 * missing (see `web/README.md`).
 */
export default defineConfig({
  testDir: "./e2e",
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: process.env.CI ? 2 : undefined,
  reporter: [["html", { outputFolder: "playwright-report", open: "never" }], ["list"]],
  use: {
    baseURL: BASE_URL,
    trace: "on-first-retry",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  webServer: {
    command: "pnpm run build && pnpm exec vite preview --port 3100 --strictPort",
    url: BASE_URL,
    reuseExistingServer: !process.env.CI,
    timeout: 180_000,
    env: {
      API_URL: process.env.API_URL ?? "http://localhost:8080/v1",
      SITE_URL: process.env.SITE_URL ?? BASE_URL,
    },
  },
});
