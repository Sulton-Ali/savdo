import { describe, expect, it } from "vitest";

import { isValidRangeInput, presetRange } from "./dateRange";

const TASHKENT = "Asia/Tashkent";

describe("presetRange", () => {
  it("today is a single-day range ending on the shop-timezone calendar date", () => {
    // 2026-03-01T20:00:00Z is already 2026-03-02 in Asia/Tashkent (UTC+5).
    const now = new Date("2026-03-01T20:00:00Z");
    expect(presetRange("today", TASHKENT, now)).toEqual({ from: "2026-03-02", to: "2026-03-02" });
  });

  it("yesterday is a single-day range ending the day before today", () => {
    const now = new Date("2026-03-02T08:00:00Z");
    expect(presetRange("yesterday", TASHKENT, now)).toEqual({
      from: "2026-03-01",
      to: "2026-03-01",
    });
  });

  it("yesterday crosses a month boundary correctly", () => {
    const now = new Date("2026-03-01T08:00:00Z");
    expect(presetRange("yesterday", TASHKENT, now)).toEqual({
      from: "2026-02-28",
      to: "2026-02-28",
    });
  });

  it("last7Days is a 7-day inclusive window ending on today", () => {
    const now = new Date("2026-03-10T08:00:00Z");
    expect(presetRange("last7Days", TASHKENT, now)).toEqual({
      from: "2026-03-04",
      to: "2026-03-10",
    });
  });

  it("thisMonth runs from the 1st of the current month through today", () => {
    const now = new Date("2026-03-15T08:00:00Z");
    expect(presetRange("thisMonth", TASHKENT, now)).toEqual({
      from: "2026-03-01",
      to: "2026-03-15",
    });
  });

  it("thisMonth on the 1st is a single-day range", () => {
    const now = new Date("2026-03-01T08:00:00Z");
    expect(presetRange("thisMonth", TASHKENT, now)).toEqual({
      from: "2026-03-01",
      to: "2026-03-01",
    });
  });

  it("resolves the calendar date per the given timezone, not UTC", () => {
    // 2026-06-15T01:00:00Z is still 2026-06-14 in America/Los_Angeles (UTC-7 in June).
    const now = new Date("2026-06-15T01:00:00Z");
    expect(presetRange("today", "America/Los_Angeles", now)).toEqual({
      from: "2026-06-14",
      to: "2026-06-14",
    });
  });

  it("defaults `now` to the current time when omitted", () => {
    const { from, to } = presetRange("today", TASHKENT);
    expect(from).toBe(to);
    expect(from).toMatch(/^\d{4}-\d{2}-\d{2}$/);
  });
});

describe("isValidRangeInput", () => {
  it("accepts a well-formed range with from before to", () => {
    expect(isValidRangeInput("2026-03-01", "2026-03-10")).toBe(true);
  });

  it("accepts a single-day range (from equals to)", () => {
    expect(isValidRangeInput("2026-03-01", "2026-03-01")).toBe(true);
  });

  it("trims surrounding whitespace before comparing", () => {
    expect(isValidRangeInput(" 2026-03-01 ", " 2026-03-10 ")).toBe(true);
  });

  it("rejects a reversed range", () => {
    expect(isValidRangeInput("2026-03-10", "2026-03-01")).toBe(false);
  });

  it("rejects a malformed date", () => {
    expect(isValidRangeInput("2026-13-01", "2026-03-10")).toBe(false);
    expect(isValidRangeInput("not-a-date", "2026-03-10")).toBe(false);
  });

  it("rejects an out-of-range calendar date a naive regex would let through", () => {
    expect(isValidRangeInput("2026-02-30", "2026-03-10")).toBe(false);
  });

  it("rejects an empty field", () => {
    expect(isValidRangeInput("", "2026-03-10")).toBe(false);
    expect(isValidRangeInput("2026-03-01", "")).toBe(false);
  });
});
