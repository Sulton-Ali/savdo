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

  it("uses the shared text-muted class for the sample pill, not a landing-only one (phase-7.5 T3)", () => {
    // The pill sits on `bg-bg` directly; web's `--color-muted` (styles.css)
    // is now AA-safe there on its own, so the earlier `landing-muted`
    // special case (T2) is gone — this pins that regression.
    render(<QuoteCard quote="Sifatli xizmat." author="[Mijoz ismi]" sampleLabel="namuna" />);
    const pill = screen.getByText("namuna");
    expect(pill.className).toContain("text-muted");
    expect(pill.className).not.toContain("landing-muted");
  });
});
