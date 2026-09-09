import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { App as AntApp } from "antd";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../../../auth/api", () => ({
  fetchTelegramLinkStatus: vi.fn(),
  createTelegramLink: vi.fn(),
  deleteTelegramLink: vi.fn(),
}));

import { createTelegramLink, deleteTelegramLink, fetchTelegramLinkStatus } from "../../../auth/api";
import { i18next } from "../../../i18n";
import { TelegramLinkPage } from "../TelegramLinkPage";

const mockedFetchStatus = vi.mocked(fetchTelegramLinkStatus);
const mockedCreateLink = vi.mocked(createTelegramLink);
const mockedDeleteLink = vi.mocked(deleteTelegramLink);

function renderPage() {
  const queryClient = new QueryClient();
  render(
    <QueryClientProvider client={queryClient}>
      <AntApp>
        <TelegramLinkPage />
      </AntApp>
    </QueryClientProvider>,
  );
}

describe("TelegramLinkPage", () => {
  beforeAll(async () => {
    await i18next.changeLanguage("en");
  });

  beforeEach(() => {
    mockedFetchStatus.mockReset();
    mockedCreateLink.mockReset();
    mockedDeleteLink.mockReset();
  });

  afterEach(() => {
    cleanup();
  });

  it("renders the not-linked state and a Link Telegram button", async () => {
    mockedFetchStatus.mockResolvedValueOnce({ linked: false, telegramUsername: null });
    renderPage();

    expect(await screen.findByText(/not linked/i)).toBeTruthy();
    expect(screen.getByRole("button", { name: /link telegram/i })).toBeTruthy();
  });

  it("renders the linked state with the username and an Unlink button", async () => {
    mockedFetchStatus.mockResolvedValueOnce({ linked: true, telegramUsername: "alice_tg" });
    renderPage();

    expect(await screen.findByText(/linked as @alice_tg/i)).toBeTruthy();
    expect(screen.getByRole("button", { name: /unlink/i })).toBeTruthy();
  });

  it("creating a link shows the code and the deep link", async () => {
    mockedFetchStatus.mockResolvedValue({ linked: false, telegramUsername: null });
    mockedCreateLink.mockResolvedValueOnce({
      code: "abc123",
      deepLink: "https://t.me/savdo_bot?start=link_abc123",
    });
    renderPage();

    fireEvent.click(await screen.findByRole("button", { name: /link telegram/i }));

    expect(await screen.findByText("abc123")).toBeTruthy();
    const link = (await screen.findByRole("link", {
      name: /open in telegram/i,
    })) as HTMLAnchorElement;
    expect(link.href).toBe("https://t.me/savdo_bot?start=link_abc123");
    expect(screen.getByText(/valid for 10 minutes/i)).toBeTruthy();
  });

  it("unlinking calls the API and returns to the not-linked state after confirming", async () => {
    mockedFetchStatus
      .mockResolvedValueOnce({ linked: true, telegramUsername: "alice_tg" })
      .mockResolvedValueOnce({ linked: false, telegramUsername: null });
    mockedDeleteLink.mockResolvedValueOnce(undefined);
    renderPage();

    fireEvent.click(await screen.findByRole("button", { name: /unlink/i }));
    // AntD `Popconfirm`'s own confirm button, default `okText` "OK".
    fireEvent.click(await screen.findByRole("button", { name: "OK" }));

    await waitFor(() => expect(mockedDeleteLink).toHaveBeenCalled());
    expect(await screen.findByText(/not linked/i)).toBeTruthy();
  });
});
