import { createRoute } from "@tanstack/react-router";

import { authenticatedRoute } from "./authenticatedRoute";
import { CategoriesPage } from "./CategoriesPage";
import { requirePermission } from "./requirePermission";

export const categoriesRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/categories",
  beforeLoad: ({ context }) => requirePermission(context.me, "catalog.write"),
  component: CategoriesPage,
});
