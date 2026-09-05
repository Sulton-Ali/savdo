import { beforeEach, describe, expect, it, vi } from "vitest";

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

// `vi.mock` above is hoisted above these imports by Vitest, so both modules
// see the mocked `expo-secure-store`.
import { setServerUrl } from "./serverUrl";
import { clearToken, getToken, isCurrentToken, peekToken, setToken } from "./token";

beforeEach(() => {
  store.clear();
});

describe("token binding (D-81)", () => {
  it("is present for the URL it was issued for", async () => {
    await setServerUrl("http://a.example/v1");
    await setToken("token-a");
    expect(await getToken()).toBe("token-a");
  });

  it("is absent for a different URL", async () => {
    await setServerUrl("http://a.example/v1");
    await setToken("token-a");
    await setServerUrl("http://b.example/v1");
    expect(await getToken()).toBeNull();
  });

  it("checks the URL passed in, not a freshly-resolved current one", async () => {
    await setServerUrl("http://a.example/v1");
    await setToken("token-a");
    expect(await getToken("http://a.example/v1")).toBe("token-a");
    expect(await getToken("http://b.example/v1")).toBeNull();
  });

  it("clears the token as a side effect of a URL mismatch", async () => {
    await setServerUrl("http://a.example/v1");
    await setToken("token-a");
    await setServerUrl("http://b.example/v1");
    await getToken();
    expect(await peekToken()).toBeNull();
  });
});

describe("peekToken (pure read)", () => {
  it("returns the raw stored token regardless of URL, without clearing it", async () => {
    await setServerUrl("http://a.example/v1");
    await setToken("token-a");
    await setServerUrl("http://b.example/v1");
    expect(await peekToken()).toBe("token-a");
    // Reading it again proves the peek above had no delete side effect.
    expect(await peekToken()).toBe("token-a");
  });

  it("returns null once nothing was ever stored", async () => {
    expect(await peekToken()).toBeNull();
  });
});

describe("clearToken", () => {
  it("removes both the token and its bound URL", async () => {
    await setServerUrl("http://a.example/v1");
    await setToken("token-a");
    await clearToken();
    expect(await peekToken()).toBeNull();
    expect(await getToken("http://a.example/v1")).toBeNull();
  });
});

describe("isCurrentToken", () => {
  it("matches the exact Bearer header for the stored token", () => {
    expect(isCurrentToken("Bearer abc", "abc")).toBe(true);
  });

  it("does not match a different token", () => {
    expect(isCurrentToken("Bearer abc", "xyz")).toBe(false);
  });

  it("does not match when the header is missing", () => {
    expect(isCurrentToken(null, "abc")).toBe(false);
    expect(isCurrentToken(undefined, "abc")).toBe(false);
  });

  it("does not match when there's no stored token", () => {
    expect(isCurrentToken("Bearer abc", null)).toBe(false);
  });
});
