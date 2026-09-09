import { render, screen } from "@testing-library/react";
import { I18nextProvider } from "react-i18next";
import { describe, expect, it } from "vitest";

import { createI18nInstance } from "../../../lib/i18n";
import { ContactsCard } from "../ContactsCard";

describe("ContactsCard", () => {
  it("renders the heading and the contacts content, en locale", () => {
    render(
      <I18nextProvider i18n={createI18nInstance("en")}>
        <ContactsCard
          title="Contacts"
          contacts={{ phone: "+998901234567", address: "Tashkent, Chilonzor" }}
          social={{ telegram: "https://t.me/savdo_demo" }}
        />
      </I18nextProvider>,
    );
    expect(screen.getByRole("heading", { level: 2, name: "Contacts" })).toBeTruthy();
    expect(screen.getByText("+998901234567")).toBeTruthy();
    expect(screen.getByText("Tashkent, Chilonzor")).toBeTruthy();
    expect(screen.getByRole("link", { name: "Open Telegram" })).toBeTruthy();
  });
});
