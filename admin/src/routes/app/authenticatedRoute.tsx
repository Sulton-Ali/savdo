import { createRoute, redirect } from "@tanstack/react-router";

import { ApiAuthError } from "../../auth/api";
import { meQueryOptions } from "../../auth/useMe";
import { rootRoute } from "../root";
import { AppLayout } from "./AppLayout";
import { AuthErrorComponent } from "./AuthErrorComponent";

/**
 * Pathless layout route (T7 route guard): every child renders inside
 * `AppLayout` only after `GET /auth/me` resolves. On an UNAUTHENTICATED
 * `GET /auth/me` (no/expired session, HTTP 401) it redirects to `/login`
 * before any child route loads. Anything else — a 500, a network failure —
 * is an outage, not a logout: it must not discard a valid session, so it is
 * rethrown to `errorComponent` instead of redirecting.
 */
export const authenticatedRoute = createRoute({
  id: "authenticated",
  getParentRoute: () => rootRoute,
  beforeLoad: async ({ context }) => {
    try {
      const me = await context.queryClient.ensureQueryData(meQueryOptions());
      return { me };
    } catch (error) {
      if (error instanceof ApiAuthError && error.code === "UNAUTHENTICATED") {
        throw redirect({ to: "/login" });
      }
      throw error;
    }
  },
  component: AppLayout,
  errorComponent: AuthErrorComponent,
});
