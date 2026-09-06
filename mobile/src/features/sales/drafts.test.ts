import { describe, expect, it } from "vitest";

import { canManageDraft, draftAge, parseUnavailableLineIndexes } from "./drafts";

describe("canManageDraft", () => {
  it("allows manager+ (canManage true) regardless of who created the draft", () => {
    expect(canManageDraft("someone-else", "u1", true)).toBe(true);
    expect(canManageDraft(null, "u1", true)).toBe(true);
  });

  it("allows the draft's own creator even without manager+", () => {
    expect(canManageDraft("u1", "u1", false)).toBe(true);
  });

  it("denies a non-manager who is not the draft's creator", () => {
    expect(canManageDraft("u2", "u1", false)).toBe(false);
  });

  it("denies a non-manager when the draft has no creator on record", () => {
    expect(canManageDraft(null, "u1", false)).toBe(false);
  });

  it("denies when the current user id is unknown (session still loading) and canManage is false", () => {
    expect(canManageDraft("u1", undefined, false)).toBe(false);
  });
});

describe("parseUnavailableLineIndexes", () => {
  it("returns [] for undefined fields", () => {
    expect(parseUnavailableLineIndexes(undefined)).toEqual([]);
  });

  it("returns [] when no field matches the unavailable-line shape", () => {
    expect(parseUnavailableLineIndexes({ locationId: "invalid" })).toEqual([]);
  });

  it("extracts a single line index", () => {
    expect(parseUnavailableLineIndexes({ "items[0].variantId": "invalid" })).toEqual([0]);
  });

  it("extracts and sorts multiple line indexes", () => {
    expect(
      parseUnavailableLineIndexes({
        "items[3].variantId": "invalid",
        "items[1].variantId": "invalid",
      }),
    ).toEqual([1, 3]);
  });

  it("ignores an unrelated field alongside a matching one", () => {
    expect(
      parseUnavailableLineIndexes({
        "items[2].variantId": "invalid",
        note: "too_long",
      }),
    ).toEqual([2]);
  });
});

describe("draftAge", () => {
  const now = new Date("2026-09-06T12:00:00.000Z");

  it("reports minutes under an hour", () => {
    expect(draftAge("2026-09-06T11:55:00.000Z", now)).toEqual({ unit: "minutes", value: 5 });
  });

  it("reports 0 minutes for a just-created draft", () => {
    expect(draftAge("2026-09-06T12:00:00.000Z", now)).toEqual({ unit: "minutes", value: 0 });
  });

  it("clamps a future createdAt (clock skew) to 0 minutes", () => {
    expect(draftAge("2026-09-06T12:05:00.000Z", now)).toEqual({ unit: "minutes", value: 0 });
  });

  it("reports hours under a day", () => {
    expect(draftAge("2026-09-06T09:00:00.000Z", now)).toEqual({ unit: "hours", value: 3 });
  });

  it("reports days at 24h and beyond", () => {
    expect(draftAge("2026-09-04T12:00:00.000Z", now)).toEqual({ unit: "days", value: 2 });
  });
});
