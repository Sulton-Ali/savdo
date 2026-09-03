import { describe, expect, it } from "vitest";
import { defaultLocale, locales, resources } from "./index.js";

/** Recursively flattens a nested dictionary into dotted keys, e.g. { a: { b: "x" } } -> ["a.b"]. */
function flattenKeys(value: unknown, prefix = ""): string[] {
  if (typeof value === "string") {
    return [prefix];
  }
  if (value && typeof value === "object") {
    return Object.entries(value as Record<string, unknown>).flatMap(([key, child]) =>
      flattenKeys(child, prefix ? `${prefix}.${key}` : key),
    );
  }
  throw new Error(`Unexpected non-string, non-object value at "${prefix}"`);
}

/** Recursively collects every leaf (string) value in a nested dictionary. */
function collectValues(value: unknown): string[] {
  if (typeof value === "string") {
    return [value];
  }
  if (value && typeof value === "object") {
    return Object.values(value as Record<string, unknown>).flatMap(collectValues);
  }
  throw new Error("Unexpected non-string, non-object value");
}

describe("locales and defaultLocale", () => {
  it("lists uz, ru, en with uz as the default", () => {
    expect(locales).toEqual(["uz", "ru", "en"]);
    expect(defaultLocale).toBe("uz");
  });
});

describe("key parity across locales", () => {
  const enKeys = flattenKeys(resources.en).sort();

  it("has at least one key", () => {
    expect(enKeys.length).toBeGreaterThan(0);
  });

  for (const locale of locales) {
    it(`"${locale}" has exactly the same keys as "en"`, () => {
      const keys = flattenKeys(resources[locale]).sort();
      expect(keys).toEqual(enKeys);
    });
  }
});

describe("dictionary values", () => {
  for (const locale of locales) {
    it(`"${locale}" has no empty string values`, () => {
      const values = collectValues(resources[locale]);
      for (const value of values) {
        expect(value.length).toBeGreaterThan(0);
      }
    });
  }
});
