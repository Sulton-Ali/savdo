import { describe, expect, it } from "vitest";

import { canManageDraft, draftAge, findUnavailableLineField } from "../draftsHelpers";

describe("draftAge", () => {
  const now = new Date("2026-09-06T12:00:00Z");

  it("buckets under a minute as justNow", () => {
    expect(draftAge("2026-09-06T11:59:31Z", now)).toEqual({ unit: "justNow", value: 0 });
  });

  it("clamps a future createdAt (clock skew) to justNow instead of going negative", () => {
    expect(draftAge("2026-09-06T12:05:00Z", now)).toEqual({ unit: "justNow", value: 0 });
  });

  it("floors whole minutes under an hour", () => {
    expect(draftAge("2026-09-06T11:55:00Z", now)).toEqual({ unit: "minutes", value: 5 });
    expect(draftAge("2026-09-06T11:56:01Z", now)).toEqual({ unit: "minutes", value: 3 });
  });

  it("floors whole hours under a day", () => {
    expect(draftAge("2026-09-06T09:30:00Z", now)).toEqual({ unit: "hours", value: 2 });
  });

  it("floors whole days at or beyond 24 hours", () => {
    expect(draftAge("2026-09-04T12:00:00Z", now)).toEqual({ unit: "days", value: 2 });
    expect(draftAge("2026-09-05T13:00:00Z", now)).toEqual({ unit: "hours", value: 23 });
    expect(draftAge("2026-09-05T12:00:00Z", now)).toEqual({ unit: "days", value: 1 });
  });
});

describe("findUnavailableLineField", () => {
  it("returns null when fields is undefined", () => {
    expect(findUnavailableLineField(undefined)).toBeNull();
  });

  it("returns null when no field names an item's variantId", () => {
    expect(findUnavailableLineField({ note: "too_long" })).toBeNull();
  });

  it("parses the index out of an items[<index>].variantId field", () => {
    expect(findUnavailableLineField({ "items[2].variantId": "invalid" })).toEqual({
      index: 2,
      reason: "invalid",
    });
  });

  it("finds the field even alongside unrelated fields", () => {
    expect(findUnavailableLineField({ note: "too_long", "items[0].variantId": "invalid" })).toEqual(
      { index: 0, reason: "invalid" },
    );
  });
});

describe("canManageDraft", () => {
  it("lets the creator manage their own draft even without manager+", () => {
    expect(canManageDraft("u1", "u1", false)).toBe(true);
  });

  it("blocks a non-creator without manager+", () => {
    expect(canManageDraft("u1", "u2", false)).toBe(false);
  });

  it("lets manager+ manage any draft regardless of creator", () => {
    expect(canManageDraft("u1", "u2", true)).toBe(true);
    expect(canManageDraft(null, "u2", true)).toBe(true);
  });

  it("blocks a non-manager from a draft with no creator on record", () => {
    expect(canManageDraft(null, "u2", false)).toBe(false);
  });
});
