import { useQuery } from "@tanstack/react-query";
import { Select, Space } from "antd";
import type { CSSProperties } from "react";
import { useEffect, useId, useState } from "react";
import { useTranslation } from "react-i18next";

import { fetchProductsPage, fetchVariants, type Variant } from "../../catalog/api";

/** Search fires only once the query is empty (clears the filter) or at
 * least 2 characters long, mirroring `ProductsListPage`. */
const MIN_QUERY_LENGTH = 2;
const SEARCH_DEBOUNCE_MS = 300;

export interface StockVariantValue {
  productId: string | null;
  variantId: string | null;
}

const EMPTY_VALUE: StockVariantValue = { productId: null, variantId: null };

/** Standard "sr-only" pattern — visually hidden but still readable by
 * assistive tech and by `aria-labelledby` references (unlike `display:
 * none`, which some ARIA implementations skip). Used only to give each
 * select its own accessible-name text when `labelId` is set (see below). */
const visuallyHiddenStyle: CSSProperties = {
  position: "absolute",
  width: 1,
  height: 1,
  padding: 0,
  margin: -1,
  overflow: "hidden",
  clip: "rect(0, 0, 0, 0)",
  whiteSpace: "nowrap",
  border: 0,
};

function formatVariantLabel(variant: Variant): string {
  const attrs = Object.entries(variant.attributes)
    .map(([key, val]) => `${key}: ${val}`)
    .join(", ");
  const parts = [attrs || null, variant.sku ? `SKU ${variant.sku}` : null].filter(
    (part): part is string => part != null,
  );
  return parts.length > 0 ? parts.join(" — ") : variant.id;
}

/**
 * Two cascading searchable selects — pick a product by name, then one of
 * its variants (SKU + attributes) — shared by the stock movements filter
 * and the adjustment/transfer drawers (T6b). `GET /products` carries no
 * variant fields (`docs/05-API.md` § Conventions, "list vs get asymmetry"),
 * so variants are only fetched once a product is chosen, via `GET
 * /products/{id}/variants`. Shaped as a single controlled `value`/`onChange`
 * pair so it drops straight into an antd `Form.Item name="..."`.
 */
export function StockVariantPicker({
  value,
  onChange,
  disabled,
  labelId,
}: {
  value?: StockVariantValue;
  onChange?: (value: StockVariantValue) => void;
  disabled?: boolean;
  /** The id of an external visible label (e.g. `FilterBar.Field`'s) whose
   * text should lead each select's accessible name, via `aria-labelledby`
   * (MINOR 1, phase-7.6 filter-bar review) — combined with this
   * component's own hidden per-select label so "Search product" and
   * "Select variant" stay distinguishable. Omit for standalone uses (the
   * adjustment/transfer drawers, `QuickSalePage`), which keep the plain
   * `aria-label` they already had. */
  labelId?: string;
}) {
  const { t } = useTranslation();
  const current = value ?? EMPTY_VALUE;
  const productLabelId = useId();
  const variantLabelId = useId();

  const [rawQuery, setRawQuery] = useState("");
  const [debouncedQuery, setDebouncedQuery] = useState("");

  useEffect(() => {
    const trimmed = rawQuery.trim();
    if (trimmed.length > 0 && trimmed.length < MIN_QUERY_LENGTH) {
      return;
    }
    const timer = setTimeout(() => setDebouncedQuery(trimmed), SEARCH_DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [rawQuery]);

  const { data: productPage, isFetching: productsLoading } = useQuery({
    queryKey: ["stock", "productSearch", debouncedQuery],
    queryFn: () => fetchProductsPage({ q: debouncedQuery || undefined }, null),
  });
  const products = productPage?.items ?? [];

  const { data: variants, isFetching: variantsLoading } = useQuery({
    queryKey: ["variants", current.productId],
    queryFn: () => fetchVariants(current.productId as string),
    enabled: current.productId != null,
  });

  return (
    <Space direction="vertical" style={{ width: "100%" }}>
      {labelId != null && (
        <span id={productLabelId} style={visuallyHiddenStyle}>
          {t("stock.picker.product")}
        </span>
      )}
      <Select
        showSearch
        allowClear
        disabled={disabled}
        aria-label={labelId == null ? t("stock.picker.product") : undefined}
        aria-labelledby={labelId != null ? `${labelId} ${productLabelId}` : undefined}
        placeholder={t("stock.picker.product")}
        value={current.productId ?? undefined}
        loading={productsLoading}
        filterOption={false}
        onSearch={setRawQuery}
        onChange={(productId: string) => onChange?.({ productId, variantId: null })}
        onClear={() => onChange?.(EMPTY_VALUE)}
        options={products.map((product) => ({ value: product.id, label: product.name }))}
      />
      {labelId != null && (
        <span id={variantLabelId} style={visuallyHiddenStyle}>
          {t("stock.picker.variant")}
        </span>
      )}
      <Select
        showSearch
        allowClear
        disabled={disabled || current.productId == null}
        aria-label={labelId == null ? t("stock.picker.variant") : undefined}
        aria-labelledby={labelId != null ? `${labelId} ${variantLabelId}` : undefined}
        placeholder={t("stock.picker.variant")}
        value={current.variantId ?? undefined}
        loading={variantsLoading}
        optionFilterProp="label"
        onChange={(variantId: string) => onChange?.({ ...current, variantId })}
        onClear={() => onChange?.({ ...current, variantId: null })}
        options={(variants ?? []).map((variant) => ({
          value: variant.id,
          label: formatVariantLabel(variant),
        }))}
      />
    </Space>
  );
}
