import { describe, expect, it } from "vitest";

import { splitParagraphs } from "./text";

describe("splitParagraphs", () => {
  it("splits on blank lines", () => {
    expect(splitParagraphs("First paragraph.\n\nSecond paragraph.")).toEqual([
      "First paragraph.",
      "Second paragraph.",
    ]);
  });

  it("keeps single line breaks inside a paragraph", () => {
    expect(splitParagraphs("Line one\nLine two")).toEqual(["Line one\nLine two"]);
  });

  it("drops empty paragraphs from extra blank lines", () => {
    expect(splitParagraphs("A\n\n\n\nB")).toEqual(["A", "B"]);
  });

  it("returns an empty array for blank input", () => {
    expect(splitParagraphs("   \n\n  ")).toEqual([]);
  });
});
