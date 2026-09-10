import { Col, Row } from "antd";
import type { ReactNode } from "react";

/** `cell` fills one grid cell (1 column below `md`, 2 from `md`, 3 from
 * `xl`); `full` always spans the whole row — TextArea, Upload, per-language
 * field groups, tables, item editors and submit rows (D-123). */
const CELL_COL_PROPS = { xs: 24, md: 12, xl: 8 } as const;
const FULL_COL_PROPS = { span: 24 } as const;

/** Horizontal-only gutter: vertical rhythm between rows already comes from
 * `Form.Item`'s own bottom margin, so a vertical gutter would double it up. */
const GUTTER = 16;

function FormGridItem({
  span = "cell",
  children,
}: {
  span?: "cell" | "full";
  children: ReactNode;
}) {
  return <Col {...(span === "full" ? FULL_COL_PROPS : CELL_COL_PROPS)}>{children}</Col>;
}

/**
 * Shared responsive grid for the admin's page forms (D-123): a form declares
 * *intent* per field — `cell` (a short input: text, number, select, tree
 * select, date picker, switch) or `full` (anything that needs the whole
 * row) — instead of each page hand-rolling its own breakpoints. A `cell`
 * that would overflow a `full` neighbour simply wraps onto its own line,
 * same as any flex row.
 *
 * Modals and drawers stay single-column (D-123) and don't use this.
 */
export function FormGrid({ children, gutter = GUTTER }: { children: ReactNode; gutter?: number }) {
  return <Row gutter={gutter}>{children}</Row>;
}

FormGrid.Item = FormGridItem;
