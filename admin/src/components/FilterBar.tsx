import { Button, Col, Row, Typography } from "antd";
import type { ReactNode } from "react";
import { useId } from "react";
import { useTranslation } from "react-i18next";

const { Text } = Typography;

/** Breakpoint spans for one `FilterBar.Field`'s `Col` (matches D-123's
 * breakpoints: `xs`/`md`/`xl`). Defaults to `xs: 24, md: 12, xl: 6`;
 * override e.g. `{ xl: 12 }` for a control that needs more room (D-124's
 * stock variant picker). */
export interface FilterBarFieldSpan {
  xs?: number;
  md?: number;
  xl?: number;
}

const DEFAULT_SPAN: Required<FilterBarFieldSpan> = { xs: 24, md: 12, xl: 6 };

export interface FilterBarFieldProps {
  label: ReactNode;
  span?: FilterBarFieldSpan;
  /** The control(s) for this field. Either a plain node — for a control
   * that already manages its own accessible name — or a render function
   * that receives this field's generated `labelId`; pass it as
   * `aria-labelledby={labelId}` on the control (or combine it with the
   * control's own label id, e.g. `${labelId} ${ownId}`, when the field
   * wraps more than one control — see `StockVariantPicker`'s `labelId`
   * prop) to tie the visible label to the control for assistive tech. */
  children: ReactNode | ((labelId: string) => ReactNode);
}

/** One labeled filter control inside a `FilterBar` — the label renders
 * above `children` (D-124), in a `Col` sized `xs 24 / md 12 / xl 6` by
 * default. The label carries an id (`useId`) so a render-prop `children`
 * can tie the control's accessible name to it via `aria-labelledby`. */
function FilterBarField({ label, span, children }: FilterBarFieldProps) {
  const resolved = { ...DEFAULT_SPAN, ...span };
  const labelId = useId();
  return (
    <Col xs={resolved.xs} md={resolved.md} xl={resolved.xl}>
      <Text id={labelId} type="secondary" style={{ display: "block", marginBottom: 4 }}>
        {label}
      </Text>
      {typeof children === "function" ? children(labelId) : children}
    </Col>
  );
}

export interface FilterBarProps {
  /** One `FilterBar.Field` per control. */
  children: ReactNode;
  /** Clears every filter — the caller owns what "cleared" means for its own
   * state (e.g. an empty search-params object). */
  onReset: () => void;
  /** Loaded item count for the result line ("N results"). */
  resultCount: number;
  /** Whether more results exist past what is loaded (a cursor page's
   * `hasNextPage`) — appends "more available" to the count line. */
  hasMore?: boolean;
}

/**
 * Shared list-page filter bar (D-124): a responsive label-above-control
 * grid, a Reset button and a result-count line. Each page supplies its own
 * controls as `FilterBar.Field` children — children-slot composition, not a
 * control-descriptor array, since it lets a page keep using whatever
 * control it already has (a plain `Select`, a `RangePicker`, a
 * page-specific component like `StockVariantPicker`) without translating it
 * into a data shape first, and the handful of pages adopting this need
 * nothing more.
 *
 * URL sync, date-range presets and API filter mapping are the caller's
 * concern, not this component's — see `admin/src/lib/dateRangePresets.ts`
 * for a reusable `RangePicker` `presets` builder, and
 * `admin/src/routes/app/StockMovementsPage.tsx` /
 * `admin/src/routes/app/stockMovementsRoute.tsx` for the full pattern
 * (route `validateSearch` -> filters -> `useCursorList`).
 *
 * Usage:
 * ```tsx
 * <FilterBar onReset={handleReset} resultCount={items.length} hasMore={hasNextPage}>
 *   <FilterBar.Field label={t("...")}>
 *     {(labelId) => (
 *       <Select
 *         allowClear
 *         aria-labelledby={labelId}
 *         value={...}
 *         onChange={...}
 *         style={{ width: "100%" }}
 *       />
 *     )}
 *   </FilterBar.Field>
 *   <FilterBar.Field label={t("...")} span={{ xl: 12 }}>
 *     <SomeWiderControl />
 *   </FilterBar.Field>
 * </FilterBar>
 * ```
 */
export function FilterBar({ children, onReset, resultCount, hasMore = false }: FilterBarProps) {
  const { t } = useTranslation();

  return (
    <div style={{ marginBottom: 16 }}>
      <Row gutter={[16, 16]} align="bottom">
        {children}
        <Col flex="none">
          <Button onClick={onReset}>{t("common.reset")}</Button>
        </Col>
      </Row>
      <Text type="secondary">
        {t("filterBar.resultCount", { count: resultCount })}
        {hasMore ? ` — ${t("filterBar.moreAvailable")}` : ""}
      </Text>
    </div>
  );
}

FilterBar.Field = FilterBarField;
