import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { FeaturedProducts } from "../FeaturedProducts";

describe("FeaturedProducts", () => {
  it("renders nothing when there are no products (no data, no section)", () => {
    const { container } = render(
      <FeaturedProducts
        title="Tavsiya etilgan mahsulotlar"
        products={[]}
        locale="uz"
        currency="UZS"
      />,
    );
    expect(container.firstChild).toBeNull();
  });
});
