import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

export default defineConfig({
  plugins: [react()],
  test: {
    // `environment: "node"` (the previous default here) cannot mount React
    // components — jsdom is needed for the `web/src/components/landing/`
    // unit tests (phase-7.5 T2) added alongside the `*.test.ts` pure-function
    // suite, which keeps working unchanged under jsdom too.
    environment: "jsdom",
    setupFiles: ["./src/test/setup.ts"],
    include: ["src/**/*.test.ts", "src/**/*.test.tsx"],
    // Same cap as admin/vite.config.ts (phase-2 T6b): unbounded forks crash
    // `make verify`'s parallel `pnpm -r test` under load.
    pool: "forks",
    maxWorkers: 2,
  },
});
