import { createRoute } from "@tanstack/react-router";

import { authenticatedRoute } from "./authenticatedRoute";
import { BotConversationDetailPage } from "./BotConversationDetailPage";
import { requirePermission } from "./requirePermission";

/** Phase 7 T6: one bot conversation's transcript. Gated by `bot.read`
 * (owner/manager, `docs/04-DATA-MODEL.md` § 7). */
export const botConversationDetailRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/bot/conversations/$id",
  beforeLoad: ({ context }) => requirePermission(context.me, "bot.read"),
  component: BotConversationDetailRouteComponent,
});

function BotConversationDetailRouteComponent() {
  const { id } = botConversationDetailRoute.useParams();
  return <BotConversationDetailPage conversationId={id} />;
}
