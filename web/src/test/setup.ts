import { cleanup } from "@testing-library/react";
import { afterEach } from "vitest";

// `@testing-library/react`'s automatic cleanup only self-registers when it
// detects Jest's global `afterEach`; under Vitest (no `test.globals: true`
// here — see `admin/vite.config.ts`'s own choice not to enable it either)
// it has to be wired by hand, or a component rendered in one test's `it()`
// block is still in `document.body` for the next one.
afterEach(() => {
  cleanup();
});
