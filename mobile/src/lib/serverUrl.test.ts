import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const store = new Map<string, string>();

vi.mock("expo-secure-store", () => ({
  getItemAsync: async (key: string) => store.get(key) ?? null,
  setItemAsync: async (key: string, value: string) => {
    store.set(key, value);
  },
  deleteItemAsync: async (key: string) => {
    store.delete(key);
  },
}));

import {
  defaultServerUrl,
  getServerUrl,
  normalizeServerUrl,
  setServerUrl,
  validateServerUrl,
} from "./serverUrl";

beforeEach(() => {
  store.clear();
});

describe("defaultServerUrl", () => {
  const original = process.env.EXPO_PUBLIC_API_URL;

  afterEach(() => {
    if (original === undefined) {
      // Restoring the pre-test env: an assignment to `undefined` would
      // coerce to the string "undefined" instead of actually unsetting it.
      delete process.env.EXPO_PUBLIC_API_URL;
    } else {
      process.env.EXPO_PUBLIC_API_URL = original;
    }
  });

  it("uses EXPO_PUBLIC_API_URL when set", () => {
    process.env.EXPO_PUBLIC_API_URL = "https://shop.example/v1";
    expect(defaultServerUrl()).toBe("https://shop.example/v1");
  });

  it("falls back to the emulator alias when unset", () => {
    // An assignment to `undefined` would coerce to the string "undefined",
    // so this deletes the key outright to simulate it truly being unset.
    delete process.env.EXPO_PUBLIC_API_URL;
    expect(defaultServerUrl()).toBe("http://10.0.2.2:8080/v1");
  });

  it("treats an empty string as unset", () => {
    process.env.EXPO_PUBLIC_API_URL = "";
    expect(defaultServerUrl()).toBe("http://10.0.2.2:8080/v1");
  });
});

describe("normalizeServerUrl", () => {
  it("trims whitespace and drops a trailing slash", () => {
    expect(normalizeServerUrl("  http://10.0.2.2:8080/v1/  ")).toBe("http://10.0.2.2:8080/v1");
  });

  it("drops multiple trailing slashes", () => {
    expect(normalizeServerUrl("http://10.0.2.2:8080/v1///")).toBe("http://10.0.2.2:8080/v1");
  });

  it("leaves an already-normalised URL untouched", () => {
    expect(normalizeServerUrl("http://10.0.2.2:8080/v1")).toBe("http://10.0.2.2:8080/v1");
  });
});

describe("validateServerUrl (D-81)", () => {
  it("accepts plain http for loopback", () => {
    expect(validateServerUrl("http://127.0.0.1:8080/v1")).toBe("valid");
    expect(validateServerUrl("http://localhost:8080/v1")).toBe("valid");
  });

  it("accepts plain http for private ranges", () => {
    expect(validateServerUrl("http://10.0.2.2:8080/v1")).toBe("valid");
    expect(validateServerUrl("http://172.16.0.5:8080/v1")).toBe("valid");
    expect(validateServerUrl("http://172.31.255.255:8080/v1")).toBe("valid");
    expect(validateServerUrl("http://192.168.1.10:8080/v1")).toBe("valid");
  });

  it("rejects plain http for a public host as insecure", () => {
    expect(validateServerUrl("http://api.savdo.uz/v1")).toBe("insecure");
  });

  it("rejects plain http for an out-of-range private-looking octet as insecure", () => {
    // 172.15.x and 172.32.x are outside the 172.16-31 private range.
    expect(validateServerUrl("http://172.15.0.1:8080/v1")).toBe("insecure");
    expect(validateServerUrl("http://172.32.0.1:8080/v1")).toBe("insecure");
  });

  it("accepts https for any host", () => {
    expect(validateServerUrl("https://api.savdo.uz/v1")).toBe("valid");
    expect(validateServerUrl("https://10.0.2.2:8080/v1")).toBe("valid");
  });

  it("rejects a hostname that merely starts with a private-looking prefix as insecure", () => {
    expect(validateServerUrl("http://10.0.2.2.evil.com/v1")).toBe("insecure");
  });

  it("rejects a query string as invalid", () => {
    expect(validateServerUrl("https://api.savdo.uz/v1?token=abc")).toBe("invalid");
  });

  it("rejects a fragment as invalid", () => {
    expect(validateServerUrl("https://api.savdo.uz/v1#section")).toBe("invalid");
  });

  it("rejects a malformed URL as invalid", () => {
    expect(validateServerUrl("not a url")).toBe("invalid");
  });

  it("rejects a non-http(s) scheme as invalid", () => {
    expect(validateServerUrl("ftp://api.savdo.uz/v1")).toBe("invalid");
  });
});

describe("getServerUrl / setServerUrl persistence", () => {
  it("returns the default when nothing is stored", async () => {
    expect(await getServerUrl()).toBe(defaultServerUrl());
  });

  it("returns a normalised, previously stored URL", async () => {
    await setServerUrl("http://10.0.2.2:8080/v1/");
    expect(await getServerUrl()).toBe("http://10.0.2.2:8080/v1");
  });
});
