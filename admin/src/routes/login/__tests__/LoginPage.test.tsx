import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from "@tanstack/react-router";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../../../auth/api", () => ({
  login: vi.fn(),
  ApiAuthError: class ApiAuthError extends Error {
    code: string;
    retryAfterSeconds?: number;
    constructor(code: string, retryAfterSeconds?: number) {
      super(code);
      this.code = code;
      this.retryAfterSeconds = retryAfterSeconds;
    }
  },
}));

import { ApiAuthError, login } from "../../../auth/api";
import { i18next } from "../../../i18n";
import { LoginPage } from "../LoginPage";

const mockedLogin = vi.mocked(login);

function renderLoginPage() {
  const rootRoute = createRootRoute({ component: () => <Outlet /> });
  const loginRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/login",
    component: LoginPage,
  });
  const homeRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/",
    component: () => <div>home</div>,
  });
  const routeTree = rootRoute.addChildren([loginRoute, homeRoute]);
  const router = createRouter({
    routeTree,
    history: createMemoryHistory({ initialEntries: ["/login"] }),
  });
  const queryClient = new QueryClient();

  render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );

  return { router };
}

describe("LoginPage", () => {
  beforeAll(async () => {
    // Deterministic English copy for the assertions below; the language
    // switcher itself is covered in `LanguageSwitcher.test.tsx`.
    await i18next.changeLanguage("en");
  });

  beforeEach(() => {
    mockedLogin.mockReset();
  });

  // `vite.config.ts` does not set `test.globals`, so testing-library's
  // automatic per-test cleanup (which hooks into a global `afterEach`)
  // never registers — do it explicitly, since this file renders more
  // than once.
  afterEach(() => {
    cleanup();
  });

  it("submits username/password and navigates to / on success", async () => {
    mockedLogin.mockResolvedValueOnce({ id: "u1" } as never);
    const { router } = renderLoginPage();

    fireEvent.change(await screen.findByLabelText(/username/i), {
      target: { value: "alice" },
    });
    fireEvent.change(screen.getByLabelText(/password/i), {
      target: { value: "hunter2222" },
    });
    fireEvent.click(screen.getByRole("button", { name: /sign in/i }));

    await waitFor(() =>
      expect(mockedLogin).toHaveBeenCalledWith({ username: "alice", password: "hunter2222" }),
    );
    await waitFor(() => expect(router.state.location.pathname).toBe("/"));
  });

  it("shows the invalid-credentials message on an UNAUTHENTICATED error", async () => {
    mockedLogin.mockRejectedValueOnce(new ApiAuthError("UNAUTHENTICATED"));
    renderLoginPage();

    fireEvent.change(await screen.findByLabelText(/username/i), {
      target: { value: "alice" },
    });
    fireEvent.change(screen.getByLabelText(/password/i), {
      target: { value: "wrongpassword" },
    });
    fireEvent.click(screen.getByRole("button", { name: /sign in/i }));

    expect(await screen.findByText(/incorrect username or password/i)).toBeTruthy();
  });

  it("shows the rate-limited message with the Retry-After hint", async () => {
    mockedLogin.mockRejectedValueOnce(new ApiAuthError("RATE_LIMITED", 30));
    renderLoginPage();

    fireEvent.change(await screen.findByLabelText(/username/i), {
      target: { value: "alice" },
    });
    fireEvent.change(screen.getByLabelText(/password/i), {
      target: { value: "hunter2222" },
    });
    fireEvent.click(screen.getByRole("button", { name: /sign in/i }));

    expect(await screen.findByText(/too many attempts/i)).toBeTruthy();
    expect(await screen.findByText("Try again in 30s")).toBeTruthy();
  });
});
