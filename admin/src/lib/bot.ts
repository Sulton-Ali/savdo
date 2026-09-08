import type { components } from "@savdo/api-client";

import { api } from "./api";
import { ApiError } from "./errors";
import { type CursorPage, useCursorList } from "./useCursorList";

export type BotConversation = components["schemas"]["BotConversation"];
export type BotConversationList = components["schemas"]["BotConversationList"];
export type BotMessage = components["schemas"]["BotMessage"];
export type BotMessageList = components["schemas"]["BotMessageList"];
export type BotMessageRole = components["schemas"]["BotMessageRole"];

/** Matches every other collection endpoint's default (`docs/05-API.md` §
 * Conventions). */
const PAGE_LIMIT = 50;

/** `GET /bot/conversations` — requires `bot.read` (owner/manager,
 * `docs/04-DATA-MODEL.md` § 7). Cursor-paginated, newest activity
 * (`lastMessageAt`) first. */
export async function fetchBotConversationsPage(
  cursor: string | null,
): Promise<CursorPage<BotConversation>> {
  const { data, error } = await api.GET("/bot/conversations", {
    params: { query: { limit: PAGE_LIMIT, cursor: cursor ?? undefined } },
  });
  if (error) {
    throw new ApiError(error);
  }
  return data;
}

/** `GET /bot/conversations/{id}/messages` — requires `bot.read`.
 * Cursor-paginated, oldest first. */
export async function fetchBotConversationMessagesPage(
  conversationId: string,
  cursor: string | null,
): Promise<CursorPage<BotMessage>> {
  const { data, error } = await api.GET("/bot/conversations/{id}/messages", {
    params: {
      path: { id: conversationId },
      query: { limit: PAGE_LIMIT, cursor: cursor ?? undefined },
    },
  });
  if (error) {
    throw new ApiError(error);
  }
  return data;
}

/** Cursor-paginated list of the shop's bot conversations, newest activity
 * first (`docs/05-API.md` § Bot). */
export function useBotConversations() {
  return useCursorList(["bot-conversations"], fetchBotConversationsPage);
}

/** Cursor-paginated transcript for one conversation, oldest first. Pages
 * flatten in fetch order, so "load more" keeps the transcript in
 * chronological order as later pages are appended. */
export function useBotConversationMessages(conversationId: string) {
  return useCursorList(["bot-conversation-messages", conversationId], (cursor) =>
    fetchBotConversationMessagesPage(conversationId, cursor),
  );
}
