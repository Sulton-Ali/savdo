import { createRoute } from "@tanstack/react-router";

import { authenticatedRoute } from "./authenticatedRoute";
import { requirePermission } from "./requirePermission";
import { SettingsPage } from "./SettingsPage";

export const settingsRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/settings",
  beforeLoad: ({ context }) => requirePermission(context.me, "shop.settings"),
  component: SettingsPage,
});
