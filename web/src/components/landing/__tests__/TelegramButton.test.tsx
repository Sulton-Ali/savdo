import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { TelegramButton } from "../TelegramButton";

describe("TelegramButton", () => {
  it("links to the given href and opens in a new tab", () => {
    render(<TelegramButton href="https://t.me/savdo_demo" label="Telegramda yozing" />);
    const link = screen.getByRole("link", { name: "Telegramda yozing" });
    expect(link.getAttribute("href")).toBe("https://t.me/savdo_demo");
    expect(link.getAttribute("target")).toBe("_blank");
    expect(link.getAttribute("rel")).toBe("noopener noreferrer");
  });
});
