import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    environment: "node",
    include: ["src/**/*.test.ts"],
    // Same cap as admin/vite.config.ts (phase-2 T6b): unbounded forks crash
    // `make verify`'s parallel `pnpm -r test` under load.
    pool: "forks",
    maxWorkers: 2,
  },
});
