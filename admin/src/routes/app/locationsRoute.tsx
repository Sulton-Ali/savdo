import { createRoute } from "@tanstack/react-router";

import { authenticatedRoute } from "./authenticatedRoute";
import { LocationsPage } from "./LocationsPage";
import { requirePermission } from "./requirePermission";

export const locationsRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/locations",
  beforeLoad: ({ context }) => requirePermission(context.me, "locations.manage"),
  component: LocationsPage,
});
