import { describe, expect, it } from "vitest";

import { formatHoursRange, weekdayTranslationKey } from "./hours";

describe("weekdayTranslationKey", () => {
  it("maps every weekday to its own key", () => {
    expect(weekdayTranslationKey("mon")).toBe("web.hours.mon");
    expect(weekdayTranslationKey("sun")).toBe("web.hours.sun");
  });
});

describe("formatHoursRange", () => {
  it("formats an open day as open–close", () => {
    expect(formatHoursRange({ day: "mon", closed: false, open: "09:00", close: "19:00" })).toBe(
      "09:00–19:00",
    );
  });

  it("returns null for a closed day", () => {
    expect(formatHoursRange({ day: "sun", closed: true })).toBeNull();
  });

  it("returns null when open/close is missing even though closed is false", () => {
    expect(formatHoursRange({ day: "mon", closed: false })).toBeNull();
    expect(formatHoursRange({ day: "mon", closed: false, open: "09:00" })).toBeNull();
  });
});
