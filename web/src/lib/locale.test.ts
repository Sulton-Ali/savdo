import { describe, expect, it } from "vitest";

import { isLocale, switchLocalePath } from "./locale";

describe("isLocale", () => {
  it("accepts uz, ru and en", () => {
    expect(isLocale("uz")).toBe(true);
    expect(isLocale("ru")).toBe(true);
    expect(isLocale("en")).toBe(true);
  });

  it("rejects anything else", () => {
    expect(isLocale("fr")).toBe(false);
    expect(isLocale("")).toBe(false);
    expect(isLocale(null)).toBe(false);
    expect(isLocale(undefined)).toBe(false);
  });
});

describe("switchLocalePath", () => {
  it("swaps the locale prefix and keeps the rest of the path", () => {
    expect(switchLocalePath("/uz/c/futbolkalar", "ru")).toBe("/ru/c/futbolkalar");
    expect(switchLocalePath("/en/p/blue-shirt", "uz")).toBe("/uz/p/blue-shirt");
  });

  it("keeps the home page when switching from the home page", () => {
    expect(switchLocalePath("/uz/", "en")).toBe("/en/");
    expect(switchLocalePath("/uz", "en")).toBe("/en");
  });

  it("falls back to the target locale's root when the path has no locale prefix", () => {
    expect(switchLocalePath("/", "ru")).toBe("/ru");
    expect(switchLocalePath("", "ru")).toBe("/ru");
  });
});
