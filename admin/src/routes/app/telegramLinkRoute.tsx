import { createRoute } from "@tanstack/react-router";

import { authenticatedRoute } from "./authenticatedRoute";
import { TelegramLinkPage } from "./TelegramLinkPage";

/**
 * Any authenticated role (deliverable C: "manager/cashier can link their
 * own account too") — no `beforeLoad` permission check, unlike the rest of
 * `/settings/*` (`settingsRoute`'s `shop.settings`,
 * `landingContentRoute`'s `content.manage`). Nested under `/settings` for
 * the sidebar grouping only; the contract's `/auth/telegram/link`
 * operations require nothing beyond a valid session.
 */
export const telegramLinkRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/settings/telegram",
  component: TelegramLinkPage,
});
