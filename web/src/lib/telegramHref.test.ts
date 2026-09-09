import { describe, expect, it } from "vitest";

import { getTelegramHref } from "./telegramHref";

describe("getTelegramHref", () => {
  it("returns the URL when telegram is a safe https link", () => {
    expect(getTelegramHref({ telegram: "https://t.me/savdo_demo" })).toBe(
      "https://t.me/savdo_demo",
    );
  });

  it("returns null when telegram is missing", () => {
    expect(getTelegramHref({})).toBeNull();
  });

  it("returns null when social itself is null or undefined", () => {
    expect(getTelegramHref(null)).toBeNull();
    expect(getTelegramHref(undefined)).toBeNull();
  });

  it("returns null for an unsafe (non-https) telegram URL", () => {
    expect(getTelegramHref({ telegram: "javascript:alert(1)" })).toBeNull();
    expect(getTelegramHref({ telegram: "http://t.me/savdo_demo" })).toBeNull();
  });
});
