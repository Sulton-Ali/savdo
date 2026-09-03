import { describe, expect, it } from "vitest";

import { healthLabel } from "./health";

describe("healthLabel", () => {
  it("reports ok when the API responds healthy", () => {
    expect(healthLabel({ data: { status: "ok" } })).toBe("API: ok");
  });

  it("reports error when the request failed", () => {
    expect(healthLabel({ error: { error: { code: "INTERNAL" } } })).toBe("API: error");
  });

  it("reports unknown when there is neither data nor error", () => {
    expect(healthLabel({})).toBe("API: unknown");
  });
});
