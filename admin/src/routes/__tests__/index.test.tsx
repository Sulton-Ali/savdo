import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { HomePage } from "../HomePage";

vi.mock("../../lib/api", () => ({
  api: {
    GET: vi.fn(async () => ({ data: { status: "ok" }, error: undefined })),
  },
}));

function renderWithClient() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <HomePage />
    </QueryClientProvider>,
  );
}

describe("HomePage", () => {
  it("shows API: ok once the healthz query resolves", async () => {
    renderWithClient();

    expect(await screen.findByText("API: ok")).toBeTruthy();
  });
});
