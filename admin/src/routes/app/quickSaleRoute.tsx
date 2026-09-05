import { createRoute } from "@tanstack/react-router";

import { authenticatedRoute } from "./authenticatedRoute";
import { QuickSalePage } from "./QuickSalePage";

/** No `beforeLoad` guard — every role, including cashier, may create a sale
 * and attach a customer (`docs/04-DATA-MODEL.md` § 7). */
export const quickSaleRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/quick-sale",
  component: QuickSalePage,
});
