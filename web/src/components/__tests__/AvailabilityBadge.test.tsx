import { render, screen } from "@testing-library/react";
import { I18nextProvider } from "react-i18next";
import { describe, expect, it } from "vitest";

import { createI18nInstance } from "../../lib/i18n";
import { AvailabilityBadge } from "../AvailabilityBadge";

describe("AvailabilityBadge", () => {
  it("hugs its label instead of stretching to a parent's full width", () => {
    render(
      <I18nextProvider i18n={createI18nInstance("uz")}>
        <AvailabilityBadge value="in_stock" />
      </I18nextProvider>,
    );
    const badge = screen.getByText("Mavjud");
    // `inline-flex` + `w-fit` stop a flex-column parent's default
    // `align-items: stretch` from filling the pill's width with its label
    // (owner report, 2026-09-09: the badge on public product cards was
    // spanning the whole card).
    expect(badge.className).toContain("inline-flex");
    expect(badge.className).toContain("w-fit");
  });

  it("accepts an extra className for the caller's own layout (e.g. self-start)", () => {
    render(
      <I18nextProvider i18n={createI18nInstance("uz")}>
        <AvailabilityBadge value="low" className="self-start" />
      </I18nextProvider>,
    );
    expect(screen.getByText("Kam qoldi").className).toContain("self-start");
  });
});
