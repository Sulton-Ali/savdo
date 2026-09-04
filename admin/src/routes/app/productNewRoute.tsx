import { createRoute } from "@tanstack/react-router";

import { authenticatedRoute } from "./authenticatedRoute";
import { ProductFormPage } from "./ProductFormPage";
import { requirePermission } from "./requirePermission";

export const productNewRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/products/new",
  beforeLoad: ({ context }) => requirePermission(context.me, "catalog.write"),
  component: () => <ProductFormPage />,
});
