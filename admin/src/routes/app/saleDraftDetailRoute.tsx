import { createRoute } from "@tanstack/react-router";

import { authenticatedRoute } from "./authenticatedRoute";
import { SaleDraftDetailPage } from "./SaleDraftDetailPage";

/** No `beforeLoad` guard — every role, including cashier, may read a draft
 * (D-87). Pay/Edit/Delete stay gated inside the page. */
export const saleDraftDetailRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/sales/drafts/$id",
  component: SaleDraftDetailRouteComponent,
});

function SaleDraftDetailRouteComponent() {
  const { id } = saleDraftDetailRoute.useParams();
  return <SaleDraftDetailPage draftId={id} />;
}
