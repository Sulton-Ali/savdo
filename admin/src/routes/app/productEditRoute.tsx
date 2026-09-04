import { createRoute } from "@tanstack/react-router";

import { authenticatedRoute } from "./authenticatedRoute";
import { ProductFormPage } from "./ProductFormPage";
import { requirePermission } from "./requirePermission";

export const productEditRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/products/$id",
  beforeLoad: ({ context }) => requirePermission(context.me, "catalog.write"),
  component: ProductEditRouteComponent,
});

function ProductEditRouteComponent() {
  const { id } = productEditRoute.useParams();
  return <ProductFormPage productId={id} />;
}
