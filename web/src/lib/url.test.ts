import { describe, expect, it } from "vitest";

import { isSafeHttpsUrl } from "./url";

describe("isSafeHttpsUrl", () => {
  it("accepts an https URL", () => {
    expect(isSafeHttpsUrl("https://t.me/savdo_demo")).toBe(true);
  });

  it("rejects a javascript: URL", () => {
    expect(isSafeHttpsUrl("javascript:alert(1)")).toBe(false);
  });

  it("rejects plain http", () => {
    expect(isSafeHttpsUrl("http://example.com")).toBe(false);
  });

  it("rejects malformed, empty and missing values", () => {
    expect(isSafeHttpsUrl("not a url")).toBe(false);
    expect(isSafeHttpsUrl("")).toBe(false);
    expect(isSafeHttpsUrl(null)).toBe(false);
    expect(isSafeHttpsUrl(undefined)).toBe(false);
  });
});
