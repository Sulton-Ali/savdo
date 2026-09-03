import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { beforeAll, describe, expect, it, vi } from "vitest";

vi.mock("../../../lib/api", () => ({
  api: {
    GET: vi.fn(async () => ({ data: { status: "ok" }, error: undefined })),
  },
}));

import { i18next } from "../../../i18n";
import { DashboardPage } from "../DashboardPage";

function renderWithClient() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <DashboardPage />
    </QueryClientProvider>,
  );
}

describe("DashboardPage", () => {
  beforeAll(async () => {
    await i18next.changeLanguage("en");
  });

  // Carried over from the Phase 0 `HomePage` test this page replaces: it
  // proves the admin app can still reach the API through the generated
  // client and the dev proxy.
  it("shows API: ok once the healthz query resolves", async () => {
    renderWithClient();

    expect(await screen.findByText("API: ok")).toBeTruthy();
  });
});
