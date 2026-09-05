import { describe, expect, it } from "vitest";

import { deriveSessionGate } from "./sessionGate";

describe("deriveSessionGate", () => {
  it("is loading while the token query itself is loading", () => {
    expect(
      deriveSessionGate({
        hasToken: false,
        tokenLoading: true,
        hasMeData: false,
        meLoading: false,
      }),
    ).toBe("loading");
  });

  it("is loading while a token exists and `me` hasn't resolved for the first time yet", () => {
    expect(
      deriveSessionGate({ hasToken: true, tokenLoading: false, hasMeData: false, meLoading: true }),
    ).toBe("loading");
  });

  it("is unauthenticated once settled with no token", () => {
    expect(
      deriveSessionGate({
        hasToken: false,
        tokenLoading: false,
        hasMeData: false,
        meLoading: false,
      }),
    ).toBe("unauthenticated");
  });

  it("is unreachable once settled with a token but no `me` data (D-81 outage screen)", () => {
    expect(
      deriveSessionGate({
        hasToken: true,
        tokenLoading: false,
        hasMeData: false,
        meLoading: false,
      }),
    ).toBe("unreachable");
  });

  it("is authenticated once `me` data exists", () => {
    expect(
      deriveSessionGate({ hasToken: true, tokenLoading: false, hasMeData: true, meLoading: false }),
    ).toBe("authenticated");
  });

  it("stays authenticated even while a refetch is in flight, as long as data is present (error-with-data stays app)", () => {
    expect(
      deriveSessionGate({ hasToken: true, tokenLoading: false, hasMeData: true, meLoading: true }),
    ).toBe("authenticated");
  });
});
