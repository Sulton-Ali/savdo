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
import { App as AntApp } from "antd";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../../../auth/api", () => ({
  requestPasswordResetOtp: vi.fn(),
  verifyPasswordResetOtp: vi.fn(),
  resetPassword: vi.fn(),
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

import {
  ApiAuthError,
  requestPasswordResetOtp,
  resetPassword,
  verifyPasswordResetOtp,
} from "../../../auth/api";
import { i18next } from "../../../i18n";
import { ForgotPasswordPage } from "../ForgotPasswordPage";

const mockedRequest = vi.mocked(requestPasswordResetOtp);
const mockedVerify = vi.mocked(verifyPasswordResetOtp);
const mockedReset = vi.mocked(resetPassword);

function renderForgotPasswordPage() {
  const rootRoute = createRootRoute({ component: () => <Outlet /> });
  const forgotPasswordRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/forgot-password",
    component: ForgotPasswordPage,
  });
  const loginRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/login",
    component: () => <div>login page</div>,
  });
  const routeTree = rootRoute.addChildren([forgotPasswordRoute, loginRoute]);
  const router = createRouter({
    routeTree,
    history: createMemoryHistory({ initialEntries: ["/forgot-password"] }),
  });
  const queryClient = new QueryClient();

  render(
    <QueryClientProvider client={queryClient}>
      <AntApp>
        <RouterProvider router={router} />
      </AntApp>
    </QueryClientProvider>,
  );

  return { router };
}

async function goToStepCode() {
  fireEvent.change(await screen.findByLabelText(/username/i), {
    target: { value: "alice" },
  });
  fireEvent.click(screen.getByRole("button", { name: /send code/i }));
  await screen.findByLabelText(/^code$/i);
}

describe("ForgotPasswordPage", () => {
  beforeAll(async () => {
    await i18next.changeLanguage("en");
  });

  beforeEach(() => {
    mockedRequest.mockReset();
    mockedVerify.mockReset();
    mockedReset.mockReset();
  });

  afterEach(async () => {
    cleanup();
    // This page always shows at least one Ant Design `Alert` (the step 1/2
    // info notice, or an error alert), and its inline Form validation
    // errors (the confirm-password mismatch test) both animate through
    // rc-motion's `useDelayState`, which schedules a ~16ms
    // requestAnimationFrame/setTimeout callback to advance the motion
    // status *after* the test that triggered it has already returned. If
    // that callback is still pending when Vitest tears down this file's
    // jsdom environment, it throws `ReferenceError: window is not defined`
    // as an unhandled error — non-deterministically, since it races the
    // next test/file's teardown — even though every assertion above
    // already passed. Flushing a short real-timer wait here, after
    // `cleanup()`, lets any such callback run while `window` still exists.
    await new Promise((resolve) => setTimeout(resolve, 50));
  });

  it("moves from username to the code step on a 202 (no enumeration hint)", async () => {
    mockedRequest.mockResolvedValueOnce(undefined);
    renderForgotPasswordPage();

    await goToStepCode();

    expect(mockedRequest).toHaveBeenCalledWith("alice");
    expect(
      screen.getByText(/if this account is linked to telegram, a code was sent/i),
    ).toBeTruthy();
  });

  it("shows the rate-limited message with Retry-After on step 1", async () => {
    mockedRequest.mockRejectedValueOnce(new ApiAuthError("RATE_LIMITED", 45));
    renderForgotPasswordPage();

    fireEvent.change(await screen.findByLabelText(/username/i), {
      target: { value: "alice" },
    });
    fireEvent.click(screen.getByRole("button", { name: /send code/i }));

    expect(await screen.findByText(/too many attempts/i)).toBeTruthy();
    expect(await screen.findByText("Try again in 45s")).toBeTruthy();
  });

  it("shows an error and stays on the code step on a wrong/expired code (401)", async () => {
    mockedRequest.mockResolvedValueOnce(undefined);
    mockedVerify.mockRejectedValueOnce(new ApiAuthError("UNAUTHENTICATED"));
    renderForgotPasswordPage();

    await goToStepCode();
    fireEvent.change(screen.getByLabelText(/^code$/i), { target: { value: "000000" } });
    fireEvent.click(screen.getByRole("button", { name: /verify code/i }));

    expect(await screen.findByText(/wrong or expired code/i)).toBeTruthy();
    expect(mockedVerify).toHaveBeenCalledWith("alice", "000000");
    // Still on step 2 — the code field is still there.
    expect(screen.getByLabelText(/^code$/i)).toBeTruthy();
  });

  it("shows the rate-limited message with Retry-After on step 2", async () => {
    mockedRequest.mockResolvedValueOnce(undefined);
    mockedVerify.mockRejectedValueOnce(new ApiAuthError("RATE_LIMITED", 10));
    renderForgotPasswordPage();

    await goToStepCode();
    fireEvent.change(screen.getByLabelText(/^code$/i), { target: { value: "123456" } });
    fireEvent.click(screen.getByRole("button", { name: /verify code/i }));

    expect(await screen.findByText(/too many attempts/i)).toBeTruthy();
    expect(await screen.findByText("Try again in 10s")).toBeTruthy();
  });

  it("completes the full flow and navigates back to /login on success", async () => {
    mockedRequest.mockResolvedValueOnce(undefined);
    mockedVerify.mockResolvedValueOnce({
      actionToken: "tok_abc",
      expiresAt: "2026-09-09T00:10:00Z",
    });
    mockedReset.mockResolvedValueOnce(undefined);
    const { router } = renderForgotPasswordPage();

    await goToStepCode();
    fireEvent.change(screen.getByLabelText(/^code$/i), { target: { value: "123456" } });
    fireEvent.click(screen.getByRole("button", { name: /verify code/i }));

    await screen.findByLabelText(/new password/i);
    fireEvent.change(screen.getByLabelText(/new password/i), {
      target: { value: "hunter2222" },
    });
    fireEvent.change(screen.getByLabelText(/confirm password/i), {
      target: { value: "hunter2222" },
    });
    fireEvent.click(screen.getByRole("button", { name: /reset password/i }));

    await waitFor(() => expect(mockedReset).toHaveBeenCalledWith("tok_abc", "hunter2222"));
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"));
  });

  it("rejects a mismatched confirm password without calling the API", async () => {
    mockedRequest.mockResolvedValueOnce(undefined);
    mockedVerify.mockResolvedValueOnce({
      actionToken: "tok_abc",
      expiresAt: "2026-09-09T00:10:00Z",
    });
    renderForgotPasswordPage();

    await goToStepCode();
    fireEvent.change(screen.getByLabelText(/^code$/i), { target: { value: "123456" } });
    fireEvent.click(screen.getByRole("button", { name: /verify code/i }));

    await screen.findByLabelText(/new password/i);
    fireEvent.change(screen.getByLabelText(/new password/i), {
      target: { value: "hunter2222" },
    });
    fireEvent.change(screen.getByLabelText(/confirm password/i), {
      target: { value: "somethingelse" },
    });
    fireEvent.click(screen.getByRole("button", { name: /reset password/i }));

    expect(await screen.findByText(/passwords do not match/i)).toBeTruthy();
    expect(mockedReset).not.toHaveBeenCalled();
  });
});
