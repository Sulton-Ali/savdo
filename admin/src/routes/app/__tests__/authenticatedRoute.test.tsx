import { QueryClient } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import { render, screen, waitFor } from "@testing-library/react";
import { beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

const fetchMeMock = vi.fn();

vi.mock("../../../auth/api", () => ({
  fetchMe: () => fetchMeMock(),
  ApiAuthError: class ApiAuthError extends Error {
    code: string;
    retryAfterSeconds?: number;
    constructor(code: string, retryAfterSeconds?: number) {
      super(code);
      this.name = "ApiAuthError";
      this.code = code;
      this.retryAfterSeconds = retryAfterSeconds;
    }
  },
}));

import { ApiAuthError } from "../../../auth/api";
import { i18next } from "../../../i18n";
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
  beforeAll(async () => {
    await i18next.changeLanguage("en");
  });

  beforeEach(() => {
    fetchMeMock.mockReset();
  });

  it("redirects to /login on an UNAUTHENTICATED GET /auth/me (a real logout)", async () => {
    fetchMeMock.mockRejectedValue(new ApiAuthError("UNAUTHENTICATED"));
    const router = buildRouter();
    render(<RouterProvider router={router} />);

    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/login");
    });
  });

  it("does not redirect on a 500 — renders the error state instead", async () => {
    fetchMeMock.mockRejectedValue(new ApiAuthError("INTERNAL"));
    const router = buildRouter();
    render(<RouterProvider router={router} />);

    expect(await screen.findByText(/service unavailable/i)).toBeTruthy();
    expect(router.state.location.pathname).toBe("/");
  });

  it("does not redirect on a network failure — renders the error state instead", async () => {
    fetchMeMock.mockRejectedValue(new TypeError("Failed to fetch"));
    const router = buildRouter();
    render(<RouterProvider router={router} />);

    expect(await screen.findByText(/service unavailable/i)).toBeTruthy();
    expect(router.state.location.pathname).toBe("/");
  });
});
