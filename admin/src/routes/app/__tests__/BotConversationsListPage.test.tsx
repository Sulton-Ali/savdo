import type { components } from "@savdo/api-client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { App as AntApp, ConfigProvider } from "antd";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

const navigateMock = vi.fn();

vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-router")>();
  return { ...actual, useNavigate: () => navigateMock };
});

vi.mock("../../../lib/api", () => ({
  api: { GET: vi.fn() },
}));

import { i18next } from "../../../i18n";
import { api } from "../../../lib/api";
import { BotConversationsListPage } from "../BotConversationsListPage";

type BotConversation = components["schemas"]["BotConversation"];

const mockedApi = vi.mocked(api, { deep: true });

function conversation(overrides: Partial<BotConversation> = {}): BotConversation {
  return {
    id: "c1",
    telegramChatId: "123456789",
    telegramUsername: "jane_doe",
    customerId: null,
    mode: "customer",
    messageCount: 4,
    lastMessageAt: "2026-09-08T10:00:00Z",
    createdAt: "2026-09-01T09:00:00Z",
    ...overrides,
  };
}

function apiResult(data: unknown, status = 200) {
  return { data, error: undefined, response: new Response(null, { status }) } as never;
}

function renderPage() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <ConfigProvider theme={{ token: { motion: false } }}>
      <QueryClientProvider client={queryClient}>
        <AntApp>
          <BotConversationsListPage />
        </AntApp>
      </QueryClientProvider>
    </ConfigProvider>,
  );
}

describe("BotConversationsListPage", () => {
  beforeAll(async () => {
    await i18next.changeLanguage("en");
  });

  beforeEach(() => {
    navigateMock.mockReset();
    mockedApi.GET.mockReset();
  });

  afterEach(() => {
    cleanup();
  });

  it("renders conversations returned by GET /bot/conversations, preferring the Telegram username", async () => {
    mockedApi.GET.mockResolvedValueOnce(apiResult({ items: [conversation()], nextCursor: null }));

    renderPage();

    expect(await screen.findByText("@jane_doe")).toBeTruthy();
    expect(screen.getByText("4")).toBeTruthy();
  });

  it("falls back to the chat id when no Telegram username is known", async () => {
    mockedApi.GET.mockResolvedValueOnce(
      apiResult({
        items: [conversation({ telegramUsername: null })],
        nextCursor: null,
      }),
    );

    renderPage();

    expect(await screen.findByText("123456789")).toBeTruthy();
  });

  it("navigates to the conversation detail page on row click", async () => {
    mockedApi.GET.mockResolvedValueOnce(apiResult({ items: [conversation()], nextCursor: null }));

    renderPage();
    fireEvent.click(await screen.findByText("@jane_doe"));

    expect(navigateMock).toHaveBeenCalledWith({
      to: "/bot/conversations/$id",
      params: { id: "c1" },
    });
  });

  it("'Load more' requests the next page with the returned cursor", async () => {
    mockedApi.GET.mockResolvedValueOnce(
      apiResult({
        items: [conversation({ id: "c1", telegramUsername: "first" })],
        nextCursor: "cursor-2",
      }),
    );
    mockedApi.GET.mockResolvedValueOnce(
      apiResult({
        items: [conversation({ id: "c2", telegramUsername: "second" })],
        nextCursor: null,
      }),
    );

    renderPage();

    await screen.findByText("@first");
    fireEvent.click(screen.getByRole("button", { name: "Load more" }));

    await screen.findByText("@second");
    const call = mockedApi.GET.mock.calls.find(
      (entry) =>
        entry[0] === "/bot/conversations" &&
        (entry[1] as never as { params: { query: { cursor?: string } } })?.params?.query?.cursor ===
          "cursor-2",
    );
    expect(call).toBeTruthy();
  });
});
