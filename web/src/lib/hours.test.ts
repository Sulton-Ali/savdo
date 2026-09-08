import { describe, expect, it } from "vitest";

import { formatHoursRange, sortHoursDays, weekdayTranslationKey } from "./hours";

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

describe("sortHoursDays", () => {
  it("sorts a shuffled list into fixed mon→sun order", () => {
    const shuffled = [
      { day: "wed", closed: false, open: "09:00", close: "19:00" },
      { day: "sun", closed: true },
      { day: "mon", closed: false, open: "09:00", close: "19:00" },
      { day: "sat", closed: false, open: "10:00", close: "18:00" },
      { day: "fri", closed: false, open: "09:00", close: "19:00" },
      { day: "tue", closed: false, open: "09:00", close: "19:00" },
      { day: "thu", closed: false, open: "09:00", close: "19:00" },
    ] as const;

    expect(sortHoursDays(shuffled).map((d) => d.day)).toEqual([
      "mon",
      "tue",
      "wed",
      "thu",
      "fri",
      "sat",
      "sun",
    ]);
  });

  it("tolerates a short list (missing days simply don't appear)", () => {
    const partial = [
      { day: "sun", closed: true },
      { day: "mon", closed: false, open: "09:00", close: "19:00" },
    ] as const;

    expect(sortHoursDays(partial).map((d) => d.day)).toEqual(["mon", "sun"]);
  });
});
