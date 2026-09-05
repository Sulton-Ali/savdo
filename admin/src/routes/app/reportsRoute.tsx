import { createRoute } from "@tanstack/react-router";

import { authenticatedRoute } from "./authenticatedRoute";
import { ReportsPage } from "./ReportsPage";

/** Visible to every authenticated role (`reports.read` manager+, or
 * `reports.own_day` cashier) — `ReportsPage` itself decides which view to
 * render, so this route has no `requirePermission` guard (same as
 * `/products`, `/customers`). */
export const reportsRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/reports",
  component: ReportsPage,
});
