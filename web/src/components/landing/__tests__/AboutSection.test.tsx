import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { AboutSection } from "../AboutSection";

describe("AboutSection", () => {
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
});
