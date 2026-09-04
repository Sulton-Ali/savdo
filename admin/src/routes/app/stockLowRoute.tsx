import { createRoute } from "@tanstack/react-router";

import { authenticatedRoute } from "./authenticatedRoute";
import { requirePermission } from "./requirePermission";
import { StockLowPage } from "./StockLowPage";

export const stockLowRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/stock/low",
  beforeLoad: ({ context }) => requirePermission(context.me, "stock.write"),
  component: StockLowPage,
});
