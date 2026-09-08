import type { components } from "@savdo/api-client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
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
import { BotConversationDetailPage } from "../BotConversationDetailPage";

type BotMessage = components["schemas"]["BotMessage"];

const mockedApi = vi.mocked(api, { deep: true });

function userMessage(overrides: Partial<BotMessage> = {}): BotMessage {
  return {
    id: "m1",
    role: "user",
    content: "Do you have blue shirts?",
    toolCalls: null,
    provider: null,
    model: null,
    inputTokens: null,
    outputTokens: null,
    latencyMs: null,
    costEstimate: null,
    createdAt: "2026-09-08T10:00:00Z",
    ...overrides,
  };
}

function assistantMessage(overrides: Partial<BotMessage> = {}): BotMessage {
  return {
    id: "m2",
    role: "assistant",
    content: "Yes, we have blue shirts in stock.",
    // `toolCalls` generates as `Record<string, never>` (openapi-typescript's
    // shape for an untyped `object` schema, same as `Error.details` in
    // `lib/errors.ts`) — cast the fixture's real shape, same approach.
    toolCalls: {
      tool: "search_products",
      args: { q: "blue shirt" },
    } as unknown as BotMessage["toolCalls"],
    provider: "anthropic",
    model: "claude-sonnet-5",
    inputTokens: 120,
    outputTokens: 45,
    latencyMs: 812,
    costEstimate: "0.001234",
    createdAt: "2026-09-08T10:00:05Z",
    ...overrides,
  };
}

function apiResult(data: unknown, status = 200) {
  return { data, error: undefined, response: new Response(null, { status }) } as never;
}

function renderPage(conversationId = "c1") {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <ConfigProvider theme={{ token: { motion: false } }}>
      <QueryClientProvider client={queryClient}>
        <AntApp>
          <BotConversationDetailPage conversationId={conversationId} />
        </AntApp>
      </QueryClientProvider>
    </ConfigProvider>,
  );
}

describe("BotConversationDetailPage", () => {
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

  it("renders messages oldest first with role labels", async () => {
    mockedApi.GET.mockResolvedValueOnce(
      apiResult({ items: [userMessage(), assistantMessage()], nextCursor: null }),
    );

    renderPage();

    const customerText = await screen.findByText("Do you have blue shirts?");
    const botText = screen.getByText("Yes, we have blue shirts in stock.");
    expect(customerText).toBeTruthy();
    expect(botText).toBeTruthy();
    // Oldest (customer) first, then the bot's reply, in DOM order.
    expect(
      customerText.compareDocumentPosition(botText) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    expect(screen.getByText("Customer")).toBeTruthy();
    expect(screen.getByText("Bot")).toBeTruthy();
  });

  it("shows a muted meta line for an assistant message with provider, model, tokens, latency and cost", async () => {
    mockedApi.GET.mockResolvedValueOnce(
      apiResult({ items: [assistantMessage()], nextCursor: null }),
    );

    renderPage();

    await screen.findByText("Yes, we have blue shirts in stock.");
    expect(
      screen.getByText("anthropic · claude-sonnet-5 · 120→45 tokens · 812 ms · $0.001234"),
    ).toBeTruthy();
  });

  it("shows no meta line for a user message", async () => {
    mockedApi.GET.mockResolvedValueOnce(apiResult({ items: [userMessage()], nextCursor: null }));

    renderPage();

    await screen.findByText("Do you have blue shirts?");
    expect(screen.queryByText(/tokens/)).toBeNull();
  });

  it("shows tool calls collapsed, expandable to raw JSON", async () => {
    mockedApi.GET.mockResolvedValueOnce(
      apiResult({ items: [assistantMessage()], nextCursor: null }),
    );

    renderPage();

    await screen.findByText("Yes, we have blue shirts in stock.");
    expect(screen.queryByText(/search_products/)).toBeNull();

    fireEvent.click(screen.getByText("Tool calls"));

    expect(await screen.findByText(/search_products/)).toBeTruthy();
  });

  it("'Load more' requests the next page and appends messages after the ones already shown", async () => {
    mockedApi.GET.mockResolvedValueOnce(
      apiResult({ items: [userMessage()], nextCursor: "cursor-2" }),
    );
    mockedApi.GET.mockResolvedValueOnce(
      apiResult({ items: [assistantMessage()], nextCursor: null }),
    );

    renderPage();

    await screen.findByText("Do you have blue shirts?");
    fireEvent.click(screen.getByRole("button", { name: "Load more" }));

    await screen.findByText("Yes, we have blue shirts in stock.");
    const call = mockedApi.GET.mock.calls.find(
      (entry) =>
        entry[0] === "/bot/conversations/{id}/messages" &&
        (entry[1] as never as { params: { query: { cursor?: string } } })?.params?.query?.cursor ===
          "cursor-2",
    );
    expect(call).toBeTruthy();
  });

  it("navigates back to the conversations list", async () => {
    mockedApi.GET.mockResolvedValueOnce(apiResult({ items: [], nextCursor: null }));

    renderPage();
    await waitFor(() => expect(screen.getByText("No messages yet.")).toBeTruthy());
    fireEvent.click(screen.getByLabelText("Back"));

    expect(navigateMock).toHaveBeenCalledWith({ to: "/bot/conversations" });
  });
});
