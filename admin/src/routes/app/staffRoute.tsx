import { createRoute } from "@tanstack/react-router";

import { authenticatedRoute } from "./authenticatedRoute";
import { requirePermission } from "./requirePermission";
import { StaffPage } from "./StaffPage";

export const staffRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/staff",
  beforeLoad: ({ context }) => requirePermission(context.me, "staff.manage"),
  component: StaffPage,
});
