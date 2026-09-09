import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { TelegramCtaSection } from "../TelegramCtaSection";

const props = {
  telegramLabel: "Telegramda yozing",
  heading: "Savol bormi? Telegramda so'rang",
  subheading: "Bir necha daqiqada javob beramiz.",
};

describe("TelegramCtaSection", () => {
  it("renders nothing when there is no safe Telegram link", () => {
    const { container } = render(<TelegramCtaSection {...props} telegramHref={null} />);
    expect(container.firstChild).toBeNull();
  });

  it("renders the heading, subheading and a working Telegram link when there is one", () => {
    render(<TelegramCtaSection {...props} telegramHref="https://t.me/savdo_demo" />);
    expect(screen.getByText(props.heading)).toBeTruthy();
    expect(screen.getByText(props.subheading)).toBeTruthy();
    const link = screen.getByRole("link", { name: props.telegramLabel });
    expect(link.getAttribute("href")).toBe("https://t.me/savdo_demo");
  });
});
