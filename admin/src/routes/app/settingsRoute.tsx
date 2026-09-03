import { createRoute } from "@tanstack/react-router";

import { authenticatedRoute } from "./authenticatedRoute";
import { ComingSoonPage } from "./ComingSoonPage";

export const settingsRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/settings",
  component: () => <ComingSoonPage titleKey="nav.settings" />,
});
