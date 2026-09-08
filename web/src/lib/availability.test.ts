import { describe, expect, it } from "vitest";

import { availabilityToneClass, availabilityTranslationKey } from "./availability";

describe("availabilityTranslationKey", () => {
  it("maps every Availability value to its own key", () => {
    expect(availabilityTranslationKey("in_stock")).toBe("web.availability.inStock");
    expect(availabilityTranslationKey("low")).toBe("web.availability.low");
    expect(availabilityTranslationKey("out_of_stock")).toBe("web.availability.outOfStock");
  });
});

describe("availabilityToneClass", () => {
  it("uses the success/warning/danger tokens in order of severity", () => {
    expect(availabilityToneClass("in_stock")).toContain("success");
    expect(availabilityToneClass("low")).toContain("warning");
    expect(availabilityToneClass("out_of_stock")).toContain("danger");
  });
});
