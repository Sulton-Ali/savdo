import type { components } from "@savdo/api-client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";

vi.mock("../../../lib/api", () => ({
  api: {
    GET: vi.fn(async () => ({ data: { status: "ok" }, error: undefined })),
  },
}));

import { AuthProvider } from "../../../auth/AuthContext";
import { i18next } from "../../../i18n";
import { DashboardPage } from "../DashboardPage";

type Me = components["schemas"]["Me"];

const me: Me = {
  user: {
    id: "u1",
    username: "owner",
    fullName: "Test Owner",
    phone: null,
    role: "owner",
    locale: "en",
    isActive: true,
    lastLoginAt: null,
    createdAt: "2026-01-01T00:00:00Z",
  },
  shop: {
    id: "s1",
    slug: "test-shop",
    name: "Test Shop",
    currency: "UZS",
    timezone: "Asia/Tashkent",
    defaultLocale: "en",
    allowNegativeStock: false,
    updateCostOnPurchase: true,
    lowStockThreshold: 2,
  },
  permissions: [],
};

function renderWithClient() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <AuthProvider me={me}>
        <DashboardPage />
      </AuthProvider>
    </QueryClientProvider>,
  );
}

describe("DashboardPage", () => {
  beforeAll(async () => {
    await i18next.changeLanguage("en");
  });

  // `vite.config.ts` does not set `test.globals`, so testing-library's
  // automatic per-test cleanup never registers — do it explicitly, since
  // this file renders more than once.
  afterEach(() => {
    cleanup();
  });

  // Carried over from the Phase 0 `HomePage` test this page replaces: it
  // proves the admin app can still reach the API through the generated
  // client and the dev proxy.
  it("shows API: ok once the healthz query resolves", async () => {
    renderWithClient();

    expect(await screen.findByText("API: ok")).toBeTruthy();
  });

  it("shows a welcome line with the user's name", async () => {
    renderWithClient();

    expect(await screen.findByText("Welcome, Test Owner")).toBeTruthy();
  });
});
