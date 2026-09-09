import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { HeroCard } from "../HeroCard";

const baseProps = {
  fallbackTitle: "Savdo Demo",
  eyebrow: "Oilaviy do'kon",
  telegramLabel: "Telegramda yozing",
  catalogLabel: "Katalogni ko'rish",
  catalogHref: "#products",
  photoLabel: "Oila fotosi — do'kon ichida",
};

describe("HeroCard", () => {
  it("falls back to the shop name and hides the tagline when there is no hero block", () => {
    render(<HeroCard {...baseProps} telegramHref={null} />);
    expect(screen.getByRole("heading", { level: 1, name: "Savdo Demo" })).toBeTruthy();
    expect(screen.getByText(baseProps.eyebrow)).toBeTruthy();
    // No real photo and no telegram link configured: the placeholder photo
    // and its label still render (D-121 "placeholder photos until the shop
    // supplies real ones"), the Telegram button does not.
    expect(screen.getByText(baseProps.photoLabel)).toBeTruthy();
    expect(screen.queryByRole("link", { name: baseProps.telegramLabel })).toBeNull();
    expect(screen.getByRole("link", { name: baseProps.catalogLabel })).toBeTruthy();
  });

  it("renders the hero title/tagline and the Telegram CTA when both are set", () => {
    render(
      <HeroCard
        {...baseProps}
        hero={{ title: "Butun oilaga yarasha kiyimlar", tagline: "Hamyonbop narxlarda" }}
        telegramHref="https://t.me/savdo_demo"
      />,
    );
    expect(
      screen.getByRole("heading", { level: 1, name: "Butun oilaga yarasha kiyimlar" }),
    ).toBeTruthy();
    expect(screen.getByText("Hamyonbop narxlarda")).toBeTruthy();
    const telegramLink = screen.getByRole("link", { name: baseProps.telegramLabel });
    expect(telegramLink.getAttribute("href")).toBe("https://t.me/savdo_demo");
  });

  it("renders a real photo instead of the placeholder when the hero has an image", () => {
    render(
      <HeroCard
        {...baseProps}
        hero={{
          title: "Savdo Demo",
          image: { thumb: "/a_thumb.webp", card: "/a_card.webp", full: "/a_full.webp" },
        }}
        telegramHref={null}
      />,
    );
    expect(screen.queryByText(baseProps.photoLabel)).toBeNull();
    expect(screen.getByRole("img", { name: "Savdo Demo" })).toBeTruthy();
  });
});
