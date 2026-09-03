import { QueryClient } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import { render, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

vi.mock("../../../auth/api", () => ({
  fetchMe: vi.fn(async () => {
    throw new Error("401 unauthenticated");
  }),
}));

import { rootRoute } from "../../root";
import { authenticatedRoute } from "../authenticatedRoute";
import { dashboardRoute } from "../dashboardRoute";

// A stub in place of the real `LoginPage` (which needs i18next initialised)
// — this test only exercises `authenticatedRoute`'s `beforeLoad` guard.
const stubLoginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/login",
  component: () => <div>login stub</div>,
});

function buildRouter() {
  const routeTree = rootRoute.addChildren([
    stubLoginRoute,
    authenticatedRoute.addChildren([dashboardRoute]),
  ]);
  return createRouter({
    routeTree,
    context: { queryClient: new QueryClient() },
    history: createMemoryHistory({ initialEntries: ["/"] }),
  });
}

describe("authenticatedRoute", () => {
  it("redirects to /login when GET /auth/me fails", async () => {
    const router = buildRouter();
    render(<RouterProvider router={router} />);

    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/login");
    });
  });
});
