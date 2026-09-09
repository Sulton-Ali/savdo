import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { QuoteCard } from "../QuoteCard";

describe("QuoteCard", () => {
  it("renders the quote, author and the sample-copy label", () => {
    render(
      <QuoteCard
        quote="Mahsulotlar sifatli, narxlari ham qulay."
        author="[Mijoz ismi]"
        sampleLabel="namuna"
      />,
    );
    expect(screen.getByText("Mahsulotlar sifatli, narxlari ham qulay.")).toBeTruthy();
    expect(screen.getByText("[Mijoz ismi]")).toBeTruthy();
    expect(screen.getByText("namuna")).toBeTruthy();
  });
});
