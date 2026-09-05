import { defineConfig } from "vitest/config";

// D-85 (amends D-73): mobile gets Vitest for pure, RN-free helpers under
// `src/lib` only — screens and hooks stay untested here (Expo Router/React
// Native aren't set up for a test DOM in this workspace). `node`, not
// `jsdom`: none of the tested modules import React Native.
export default defineConfig({
  test: {
    environment: "node",
    include: ["src/lib/**/*.test.ts"],
  },
});
