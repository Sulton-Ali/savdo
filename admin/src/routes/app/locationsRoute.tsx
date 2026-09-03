import { createRoute } from "@tanstack/react-router";

import { authenticatedRoute } from "./authenticatedRoute";
import { ComingSoonPage } from "./ComingSoonPage";

export const locationsRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/locations",
  component: () => <ComingSoonPage titleKey="nav.locations" />,
});
