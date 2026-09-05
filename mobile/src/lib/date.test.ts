import { describe, expect, it } from "vitest";

import { calendarDateInTimeZone } from "./date";

describe("calendarDateInTimeZone", () => {
  it("resolves the calendar date in the given timezone, not UTC", () => {
    // 2026-03-01T20:00:00Z is already 2026-03-02 in Asia/Tashkent (UTC+5).
    expect(calendarDateInTimeZone(new Date("2026-03-01T20:00:00Z"), "Asia/Tashkent")).toBe(
      "2026-03-02",
    );
  });

  it("stays on the previous UTC day when the timezone is behind UTC", () => {
    // 2026-06-15T01:00:00Z is still 2026-06-14 in America/Los_Angeles (UTC-7 in June).
    expect(calendarDateInTimeZone(new Date("2026-06-15T01:00:00Z"), "America/Los_Angeles")).toBe(
      "2026-06-14",
    );
  });

  it("matches the UTC calendar date in the UTC timezone", () => {
    expect(calendarDateInTimeZone(new Date("2026-01-01T00:00:00Z"), "UTC")).toBe("2026-01-01");
  });
});
