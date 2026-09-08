import tailwindcss from "@tailwindcss/vite";
import { tanstackStart } from "@tanstack/react-start/plugin/vite";
import viteReact from "@vitejs/plugin-react";
import { defineConfig } from "vite";

export default defineConfig({
  resolve: { tsconfigPaths: true },
  plugins: [
    tailwindcss(),
    tanstackStart({ router: { quoteStyle: "double", semicolons: true } }),
    viteReact(),
  ],
  server: {
    proxy: {
      // Dev-only proxy for product/hero images. `MediaUrls` values are
      // site-relative (`/media/<shop>/<yyyy>/<mm>/<id>_<size>.webp`,
      // ADR-008) because in production Caddy serves the API's `/media/*`
      // on the same host as the landing (`docs/07-DEVOPS.md` §
      // Production) — no rewrite needed, unlike `admin/vite.config.ts`'s
      // `/api` proxy, since the API already mounts media at `/media`
      // (not under `/v1`). Never make these URLs absolute in app code.
      "/media": {
        target: "http://localhost:8080",
        changeOrigin: true,
      },
    },
  },
});
