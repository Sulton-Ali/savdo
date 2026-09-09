import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { AboutCard } from "../AboutCard";

describe("AboutCard", () => {
  it("renders the eyebrow, heading and body", () => {
    render(
      <AboutCard
        eyebrow="Biz haqimizda"
        title="Bizning oilaviy do'konimiz"
        body="Savdo Demo — Toshkentdagi oilaviy kiyim-kechak do'koni."
      />,
    );
    expect(screen.getByText("Biz haqimizda")).toBeTruthy();
    expect(
      screen.getByRole("heading", { level: 2, name: "Bizning oilaviy do'konimiz" }),
    ).toBeTruthy();
    expect(
      screen.getByText("Savdo Demo — Toshkentdagi oilaviy kiyim-kechak do'koni."),
    ).toBeTruthy();
  });

  it("renders every line at full opacity (review: /85 and /95 dropped contrast below WCAG AA)", () => {
    render(<AboutCard eyebrow="Biz haqimizda" title="Sarlavha" body="Matn." />);
    expect(screen.getByText("Biz haqimizda").className).not.toMatch(/\/\d/);
    expect(screen.getByText("Matn.").className).not.toMatch(/\/\d/);
  });
});
