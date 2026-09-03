import { fireEvent, render, screen } from "@testing-library/react";
import { useTranslation } from "react-i18next";
import { beforeAll, beforeEach, describe, expect, it } from "vitest";

import { i18next } from "../../i18n";
import { LanguageSwitcher } from "../LanguageSwitcher";

function TranslatedLabel() {
  // `useTranslation` (not a bare `i18next.t()` call) so this component
  // re-renders when `LanguageSwitcher` changes the language. `common.hello`
  // is a pre-existing key so this test only exercises i18n wiring, not the
  // Phase 1 key additions (`packages/i18n`, added separately).
  const { t } = useTranslation();
  return <span data-testid="hello-label">{t("common.hello")}</span>;
}

describe("LanguageSwitcher", () => {
  beforeAll(async () => {
    await i18next.changeLanguage("en");
  });

  beforeEach(() => {
    localStorage.clear();
  });

  it("switches the rendered language and persists the choice", async () => {
    render(
      <>
        <LanguageSwitcher />
        <TranslatedLabel />
      </>,
    );

    expect(screen.getByTestId("hello-label").textContent).toBe("Hello");

    fireEvent.click(screen.getByText("Русский"));

    expect(await screen.findByText("Привет")).toBeTruthy();
    expect(localStorage.getItem("savdo.locale")).toBe("ru");

    // Reset back to English so later tests in other files that assume the
    // default English copy are not affected by this instance's language.
    await i18next.changeLanguage("en");
  });
});
