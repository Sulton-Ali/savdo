import { createRoute } from "@tanstack/react-router";

import { authenticatedRoute } from "./authenticatedRoute";
import { ComingSoonPage } from "./ComingSoonPage";

export const staffRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/staff",
  component: () => <ComingSoonPage titleKey="nav.staff" />,
});
