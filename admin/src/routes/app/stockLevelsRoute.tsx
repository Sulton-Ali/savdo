import { createRoute } from "@tanstack/react-router";

import { authenticatedRoute } from "./authenticatedRoute";
import { StockLevelsPage } from "./StockLevelsPage";

/** No `beforeLoad` guard — every role, including cashier, sees stock
 * quantities (D-40). Write actions (adjust/transfer) stay gated inside the
 * page by `stock.write`. */
export const stockLevelsRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/stock",
  component: StockLevelsPage,
});
