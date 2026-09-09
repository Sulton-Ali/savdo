import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { AboutSection } from "../AboutSection";

describe("AboutSection", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("renders the about card and every sample quote", () => {
    render(
      <AboutSection
        eyebrow="Biz haqimizda"
        title="Bizning oilaviy do'konimiz"
        body="Qisqacha maʼlumot."
        sampleLabel="namuna"
        quotes={[
          { text: "Sifatli va arzon.", author: "[Mijoz ismi 1]" },
          { text: "Tez javob berishdi.", author: "[Mijoz ismi 2]" },
        ]}
      />,
    );
    expect(
      screen.getByRole("heading", { level: 2, name: "Bizning oilaviy do'konimiz" }),
    ).toBeTruthy();
    expect(screen.getByText("Sifatli va arzon.")).toBeTruthy();
    expect(screen.getByText("[Mijoz ismi 1]")).toBeTruthy();
    expect(screen.getByText("Tez javob berishdi.")).toBeTruthy();
    expect(screen.getByText("[Mijoz ismi 2]")).toBeTruthy();
    expect(screen.getAllByText("namuna")).toHaveLength(2);
  });

  // Regression test (review): the real homepage passes two quotes with the
  // identical placeholder author "[Mijoz ismi]" (see `$locale/index.tsx`)
  // — a `key={quote.author}` would collide and React would warn. Keying by
  // `quote.text` instead must not warn even when every author matches.
  it("does not produce a duplicate-key warning when every quote shares an author", () => {
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => {});
    render(
      <AboutSection
        eyebrow="Biz haqimizda"
        title="Bizning oilaviy do'konimiz"
        body="Qisqacha maʼlumot."
        sampleLabel="namuna"
        quotes={[
          { text: "Sifatli va arzon.", author: "[Mijoz ismi]" },
          { text: "Tez javob berishdi.", author: "[Mijoz ismi]" },
        ]}
      />,
    );
    expect(screen.getAllByText("[Mijoz ismi]")).toHaveLength(2);
    const keyWarning = consoleError.mock.calls.some(
      (call) => String(call[0]).includes("unique") && String(call[0]).toLowerCase().includes("key"),
    );
    expect(keyWarning).toBe(false);
  });
});
