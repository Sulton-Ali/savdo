import { defineConfig } from "vitest/config";

// `src/lib` and `src/features/**` only — screens and hooks stay untested
// here (Expo Router/React Native aren't set up for a test DOM in this
// workspace). `node`, not `jsdom`: none of the tested modules import React
// Native. `src/features/**/*.test.ts` was added in Phase 5 T3 for
// `features/catalog/edit/form.ts`'s pure PATCH-mapping helpers, and covers
// T4's cart math and T5's purchase-draft reducer/adjustment-form validation
// too.
export default defineConfig({
  test: {
    environment: "node",
    include: ["src/lib/**/*.test.ts", "src/features/**/*.test.ts"],
  },
});
