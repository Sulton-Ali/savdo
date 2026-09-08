import { createRoute } from "@tanstack/react-router";

import { authenticatedRoute } from "./authenticatedRoute";
import { BotConversationsListPage } from "./BotConversationsListPage";
import { requirePermission } from "./requirePermission";

/** Phase 7 T6: bot conversations list. Gated by `bot.read`
 * (owner/manager, `docs/04-DATA-MODEL.md` § 7). */
export const botConversationsRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/bot/conversations",
  beforeLoad: ({ context }) => requirePermission(context.me, "bot.read"),
  component: BotConversationsListPage,
});
