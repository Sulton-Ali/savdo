import { QueryClientProvider, useQuery } from "@tanstack/react-query";
import { act, render, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { i18next } from "../../i18n";
import { queryClient } from "../../lib/queryClient";
import { AppConfigProvider } from "../AppConfigProvider";

function CategoriesProbe({ queryFn }: { queryFn: () => Promise<unknown> }) {
  useQuery({ queryKey: ["categories", true], queryFn });
  return null;
}

/**
 * D-39: cached, locale-resolved data (categories, products, …) must refetch
 * on a language switch instead of staying stale until a reload. This
 * exercises the real `queryClient` singleton `AppConfigProvider` invalidates
 * on `languageChanged` — the same instance `main.tsx` hands to
 * `QueryClientProvider` — so the assertion matches what actually happens in
 * the app.
 */
describe("AppConfigProvider", () => {
  beforeEach(async () => {
    await i18next.changeLanguage("en");
    queryClient.clear();
  });

  afterEach(async () => {
    await i18next.changeLanguage("en");
    queryClient.clear();
  });

  it("refetches an already-cached query (e.g. categories) when the language changes", async () => {
    const queryFn = vi.fn().mockResolvedValue({ items: [] });

    render(
      <QueryClientProvider client={queryClient}>
        <AppConfigProvider>
          <CategoriesProbe queryFn={queryFn} />
        </AppConfigProvider>
      </QueryClientProvider>,
    );

    await waitFor(() => expect(queryFn).toHaveBeenCalledTimes(1));

    await act(async () => {
      await i18next.changeLanguage("ru");
    });

    await waitFor(() => expect(queryFn).toHaveBeenCalledTimes(2));
  });
});
