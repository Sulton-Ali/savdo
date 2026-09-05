import { describe, expect, it } from "vitest";

import { periodRange } from "./period";

const TASHKENT = "Asia/Tashkent";

describe("periodRange", () => {
  it("today is a single-day range ending on the shop-timezone calendar date", () => {
    // 2026-03-01T20:00:00Z is already 2026-03-02 in Asia/Tashkent (UTC+5).
    const now = new Date("2026-03-01T20:00:00Z");
    expect(periodRange("today", TASHKENT, now)).toEqual({ from: "2026-03-02", to: "2026-03-02" });
  });

  it("last7Days is a 7-day inclusive window ending on today", () => {
    const now = new Date("2026-03-10T08:00:00Z");
    expect(periodRange("last7Days", TASHKENT, now)).toEqual({
      from: "2026-03-04",
      to: "2026-03-10",
    });
  });

  it("last7Days crosses a month boundary correctly", () => {
    const now = new Date("2026-03-03T08:00:00Z");
    expect(periodRange("last7Days", TASHKENT, now)).toEqual({
      from: "2026-02-25",
      to: "2026-03-03",
    });
  });

  it("last7Days crosses a year boundary correctly", () => {
    const now = new Date("2026-01-02T08:00:00Z");
    expect(periodRange("last7Days", TASHKENT, now)).toEqual({
      from: "2025-12-27",
      to: "2026-01-02",
    });
  });

  it("resolves the calendar date per the given timezone, not UTC", () => {
    // 2026-06-15T01:00:00Z is still 2026-06-14 in America/Los_Angeles (UTC-7 in June).
    const now = new Date("2026-06-15T01:00:00Z");
    expect(periodRange("today", "America/Los_Angeles", now)).toEqual({
      from: "2026-06-14",
      to: "2026-06-14",
    });
  });

  it("defaults `now` to the current time when omitted", () => {
    const { from, to } = periodRange("today", TASHKENT);
    expect(from).toBe(to);
    expect(from).toMatch(/^\d{4}-\d{2}-\d{2}$/);
  });
});
