import { describe, expect, it } from "vitest";

import { resolveEffectivePrice } from "../quick-sale/pricing";

const NOW = new Date("2026-06-15T12:00:00Z");

describe("resolveEffectivePrice", () => {
  it("uses the product's basePrice when there is no override and no promo", () => {
    const price = resolveEffectivePrice(
      { basePrice: "100000.00", promoPrice: null, promoFrom: null, promoTo: null },
      { priceOverride: null },
      "UTC",
      NOW,
    );
    expect(price).toBe("100000.00");
  });

  it("uses the variant's priceOverride over the basePrice when there is no active promo", () => {
    const price = resolveEffectivePrice(
      { basePrice: "100000.00", promoPrice: null, promoFrom: null, promoTo: null },
      { priceOverride: "120000.00" },
      "UTC",
      NOW,
    );
    expect(price).toBe("120000.00");
  });

  it("uses the promo price when it is currently active, even over a priceOverride", () => {
    const price = resolveEffectivePrice(
      {
        basePrice: "100000.00",
        promoPrice: "80000.00",
        promoFrom: "2026-06-01T00:00:00Z",
        promoTo: "2026-06-30T23:59:59Z",
      },
      { priceOverride: "120000.00" },
      "UTC",
      NOW,
    );
    expect(price).toBe("80000.00");
  });

  it("ignores a promo that has not started yet", () => {
    const price = resolveEffectivePrice(
      {
        basePrice: "100000.00",
        promoPrice: "80000.00",
        promoFrom: "2026-07-01T00:00:00Z",
        promoTo: "2026-07-31T23:59:59Z",
      },
      { priceOverride: null },
      "UTC",
      NOW,
    );
    expect(price).toBe("100000.00");
  });

  it("ignores a promo that has already ended", () => {
    const price = resolveEffectivePrice(
      {
        basePrice: "100000.00",
        promoPrice: "80000.00",
        promoFrom: "2026-05-01T00:00:00Z",
        promoTo: "2026-05-31T23:59:59Z",
      },
      { priceOverride: null },
      "UTC",
      NOW,
    );
    expect(price).toBe("100000.00");
  });

  // D-68: promoFrom/promoTo are calendar-day bounds in the shop timezone,
  // time part ignored — a promo ending at "2026-06-15T10:00:00Z" (15:00 in
  // Asia/Tashkent, UTC+5) is still active right up through the end of that
  // same calendar day in Tashkent, and only turns inactive once Tashkent's
  // clock reaches the 16th, even though that's still the 15th in UTC.
  const product = {
    basePrice: "100000.00",
    promoPrice: "80000.00",
    promoFrom: "2026-06-01T00:00:00Z",
    promoTo: "2026-06-15T10:00:00Z",
  };
  const variant = { priceOverride: null };

  it("D-68: is still active at 23:00 Asia/Tashkent on the promo's last day", () => {
    const price = resolveEffectivePrice(
      product,
      variant,
      "Asia/Tashkent",
      new Date("2026-06-15T18:00:00Z"), // 23:00 in Asia/Tashkent (UTC+5)
    );
    expect(price).toBe("80000.00");
  });

  it("D-68: is inactive at 00:01 Asia/Tashkent the day after the promo ends", () => {
    const price = resolveEffectivePrice(
      product,
      variant,
      "Asia/Tashkent",
      new Date("2026-06-15T19:01:00Z"), // 00:01 the next day in Asia/Tashkent
    );
    expect(price).toBe("100000.00");
  });
});
