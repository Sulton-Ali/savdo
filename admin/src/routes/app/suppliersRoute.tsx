import { createRoute } from "@tanstack/react-router";

import { authenticatedRoute } from "./authenticatedRoute";
import { requirePermission } from "./requirePermission";
import { SuppliersPage } from "./SuppliersPage";

export const suppliersRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/suppliers",
  beforeLoad: ({ context }) => requirePermission(context.me, "suppliers.manage"),
  component: SuppliersPage,
});
