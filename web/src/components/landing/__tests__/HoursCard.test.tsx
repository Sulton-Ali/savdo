import { render, screen } from "@testing-library/react";
import { I18nextProvider } from "react-i18next";
import { describe, expect, it } from "vitest";

import { createI18nInstance } from "../../../lib/i18n";
import { HoursCard } from "../HoursCard";

describe("HoursCard", () => {
  it("renders the heading and the hours table content, en locale", () => {
    render(
      <I18nextProvider i18n={createI18nInstance("en")}>
        <HoursCard
          title="Opening hours"
          hours={{
            days: [
              { day: "mon", closed: false, open: "09:00", close: "19:00" },
              { day: "sun", closed: true },
            ],
          }}
        />
      </I18nextProvider>,
    );
    expect(screen.getByRole("heading", { level: 2, name: "Opening hours" })).toBeTruthy();
    expect(screen.getByText("Monday")).toBeTruthy();
    expect(screen.getByText("09:00–19:00")).toBeTruthy();
    expect(screen.getByText("Closed")).toBeTruthy();
  });
});
