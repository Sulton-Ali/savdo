import { createRoute } from "@tanstack/react-router";

import { authenticatedRoute } from "./authenticatedRoute";
import { LandingContentPage } from "./LandingContentPage";
import { requirePermission } from "./requirePermission";

export const landingContentRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/settings/landing",
  beforeLoad: ({ context }) => requirePermission(context.me, "content.manage"),
  component: LandingContentPage,
});
