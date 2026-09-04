import { createRoute } from "@tanstack/react-router";
import { AttributesPage } from "./AttributesPage";
import { authenticatedRoute } from "./authenticatedRoute";
import { requirePermission } from "./requirePermission";

export const attributesRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/settings/attributes",
  beforeLoad: ({ context }) => requirePermission(context.me, "catalog.write"),
  component: AttributesPage,
});
