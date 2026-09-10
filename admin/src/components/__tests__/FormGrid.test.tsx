import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { FormGrid } from "../FormGrid";

describe("FormGrid", () => {
  it("gives a cell item antd's 1/2/3-column responsive classes (D-123: 1 col below md, 2 from md, 3 from xl)", () => {
    render(
      <FormGrid>
        <FormGrid.Item>
          <span data-testid="cell-child">cell</span>
        </FormGrid.Item>
      </FormGrid>,
    );

    const col = screen.getByTestId("cell-child").parentElement as HTMLElement;
    expect(col.className).toContain("ant-col-xs-24");
    expect(col.className).toContain("ant-col-md-12");
    expect(col.className).toContain("ant-col-xl-8");
  });

  it("gives a full item a plain 24-column span, not a breakpoint class (D-123: TextArea, Upload, per-language groups, tables, item editors, submit rows)", () => {
    render(
      <FormGrid>
        <FormGrid.Item span="full">
          <span data-testid="full-child">full</span>
        </FormGrid.Item>
      </FormGrid>,
    );

    const col = screen.getByTestId("full-child").parentElement as HTMLElement;
    expect(col.className).toContain("ant-col-24");
    expect(col.className).not.toContain("ant-col-xs-24");
    expect(col.className).not.toContain("ant-col-md-12");
  });

  it("defaults to a cell when span is omitted", () => {
    render(
      <FormGrid>
        <FormGrid.Item>
          <span data-testid="default-child">default</span>
        </FormGrid.Item>
      </FormGrid>,
    );

    const col = screen.getByTestId("default-child").parentElement as HTMLElement;
    expect(col.className).toContain("ant-col-md-12");
  });

  it("wraps children in antd's Row with a horizontal-only gutter (no vertical gutter, Form.Item already supplies row spacing)", () => {
    render(
      <FormGrid>
        <FormGrid.Item>
          <span data-testid="row-child">child</span>
        </FormGrid.Item>
      </FormGrid>,
    );

    const row = screen.getByTestId("row-child").closest(".ant-row") as HTMLElement;
    expect(row).toBeTruthy();
    expect(row.style.rowGap).toBe("");
  });
});
