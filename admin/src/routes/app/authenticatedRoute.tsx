import { createRoute, redirect } from "@tanstack/react-router";

import { meQueryOptions } from "../../auth/useMe";
import { rootRoute } from "../root";
import { AppLayout } from "./AppLayout";

/**
 * Pathless layout route (T7 route guard): every child renders inside
 * `AppLayout` only after `GET /auth/me` resolves. On a 401 (no/expired
 * session) it redirects to `/login` before any child route loads.
 */
export const authenticatedRoute = createRoute({
  id: "authenticated",
  getParentRoute: () => rootRoute,
  beforeLoad: async ({ context }) => {
    try {
      const me = await context.queryClient.ensureQueryData(meQueryOptions());
      return { me };
    } catch {
      throw redirect({ to: "/login" });
    }
  },
  component: AppLayout,
});
