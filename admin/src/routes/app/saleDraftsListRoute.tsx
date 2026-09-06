import { createRoute } from "@tanstack/react-router";

import { authenticatedRoute } from "./authenticatedRoute";
import { SaleDraftsListPage } from "./SaleDraftsListPage";

/** No `beforeLoad` guard — every role, including cashier, may list every
 * draft (D-87, the "Create sale" row of the permission matrix,
 * `docs/04-DATA-MODEL.md` § 7). Registered as a static child of `/sales`
 * before the dynamic `saleDetailRoute` (`/sales/$id`), so it never gets
 * shadowed by that param segment (TanStack Router matches static path
 * segments before dynamic ones regardless of registration order). */
export const saleDraftsListRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/sales/drafts",
  component: SaleDraftsListPage,
});
