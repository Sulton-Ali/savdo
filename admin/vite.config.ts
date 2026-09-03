import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

// Dev proxy mirrors the production Caddy route (docs/07-DEVOPS.md § Production):
// Caddy sends `/api/*` to the API with the prefix kept, but the API itself is
// mounted at `/v1` (contracts/openapi.yaml), so the browser calls
// `/api/v1/healthz` and this proxy strips `/api` before forwarding to
// `http://localhost:8080/v1/healthz` — no CORS needed in dev.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    port: 5173,
    proxy: {
      "/api": {
        target: "http://localhost:8080",
        changeOrigin: true,
        rewrite: (path) => path.replace(/^\/api/, ""),
      },
    },
  },
  test: {
    environment: "jsdom",
    setupFiles: ["./src/test/setup.ts"],
  },
});
