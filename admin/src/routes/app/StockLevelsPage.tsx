import { useQueries, useQuery } from "@tanstack/react-query";
import { Button, Card, Select, Space, Table } from "antd";
import type { ColumnsType } from "antd/es/table";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";

import { useAuth } from "../../auth/AuthContext";
import { fetchProduct, fetchProductsPage, type Product } from "../../catalog/api";
import { FilterBar } from "../../components/FilterBar";
import { useCursorList } from "../../lib/useCursorList";
import { useDebouncedValue } from "../../lib/useDebouncedValue";
import { fetchAllLocations, fetchStockLevelsPage, formatQty, sumQty } from "../../stock/api";
import { StockAdjustmentDrawer, StockTransferDrawer } from "./StockActionsDrawer";
import type { StockLevelsSearch } from "./stockLevelsRoute";

/** Mirrors `ProductsListPage`'s search: fires only once empty (clears the
 * filter) or at least 2 characters long, 300ms after the last keystroke. */
const MIN_QUERY_LENGTH = 2;
const SEARCH_DEBOUNCE_MS = 300;

interface LevelRow {
  variantId: string;
  productId: string;
  productName: string;
  sku: string | null;
  attributesLabel: string;
  qtyByLocation: Record<string, string>;
}

export interface StockLevelsPageProps {
  /** Validated filter state from the route's search params
   * (`stockLevelsRoute`'s `validateSearch`). */
  search: StockLevelsSearch;
  /** Replaces the filter state — the caller (`stockLevelsRoute`) turns this
   * into a `navigate({ search, replace: true })` call so reload and share
   * restore it, while Back leaves the page instead of undoing one filter at
   * a time (D-124). */
  onSearchChange: (next: StockLevelsSearch) => void;
}

/**
 * Stock levels grid — one row per variant, one column per location, plus a
 * total (D-40: every authenticated role, cashier included, sees exact
 * quantities; no cost anywhere on this page). `GET /stock/levels` carries
 * only `variantId`/`productId`/`locationId`/`qty` (`StockLevel`, checked in
 * `packages/api-client/src/schema.d.ts` before writing this) — product name,
 * variant SKU and attributes are joined client-side from `GET
 * /products/{id}` (always includes `variants` for any role, `catalog/products.go`).
 *
 * The product filter lives in the route's search params as `productId`
 * only (D-124) — there is no product name in the URL, so on reload (or a
 * shared link) the select's label is re-seeded with a one-off `GET
 * /products/{id}` lookup; a 404/error there drops `productId` from the
 * search rather than leaving a broken filter behind.
 */
export function StockLevelsPage({ search, onSearchChange }: StockLevelsPageProps) {
  const { t } = useTranslation();
  const { can } = useAuth();
  const canWrite = can("stock.write");

  const [rawQuery, setRawQuery] = useState("");
  const debouncedQuery = useDebouncedValue(rawQuery, SEARCH_DEBOUNCE_MS);
  const [adjustOpen, setAdjustOpen] = useState(false);
  const [transferOpen, setTransferOpen] = useState(false);

  const trimmedQuery = debouncedQuery.trim();
  const effectiveQuery =
    trimmedQuery.length === 0 || trimmedQuery.length >= MIN_QUERY_LENGTH ? trimmedQuery : "";

  const { data: productPage } = useQuery({
    queryKey: ["stock", "productSearch", effectiveQuery],
    queryFn: () => fetchProductsPage({ q: effectiveQuery || undefined }, null),
  });
  const productOptions = (productPage?.items ?? []).map((product) => ({
    value: product.id,
    label: product.name,
  }));

  // The selected product might not be in the current search page's results
  // (e.g. right after a reload, before the user has typed anything) — seed
  // its label with a direct lookup so the select does not show a blank or
  // raw id.
  const needsProductSeed =
    search.productId != null && !productOptions.some((option) => option.value === search.productId);
  const { data: seededProduct, isError: seedFailed } = useQuery({
    queryKey: ["stock", "levelsProductSeed", search.productId],
    queryFn: () => fetchProduct(search.productId as string),
    enabled: needsProductSeed,
    retry: false,
  });

  useEffect(() => {
    if (seedFailed) {
      onSearchChange({ ...search, productId: undefined });
    }
  }, [seedFailed, search, onSearchChange]);

  const combinedProductOptions = useMemo(() => {
    if (
      seededProduct == null ||
      productOptions.some((option) => option.value === seededProduct.id)
    ) {
      return productOptions;
    }
    return [...productOptions, { value: seededProduct.id, label: seededProduct.name }];
  }, [productOptions, seededProduct]);

  const { data: locations } = useQuery({
    queryKey: ["locations", "all"],
    queryFn: fetchAllLocations,
  });

  const filters = { productId: search.productId, locationId: search.locationId };
  const { data, fetchNextPage, hasNextPage, isFetchingNextPage, isPending } = useCursorList(
    ["stock", "levels", filters],
    (cursor) => fetchStockLevelsPage(filters, cursor),
  );
  const rawLevels = useMemo(() => data?.pages.flatMap((page) => page.items) ?? [], [data]);

  const productIds = useMemo(
    () => Array.from(new Set(rawLevels.map((level) => level.productId))),
    [rawLevels],
  );
  const productQueries = useQueries({
    queries: productIds.map((id) => ({
      queryKey: ["product", id],
      queryFn: () => fetchProduct(id),
    })),
  });
  const productsById = useMemo(() => {
    const map = new Map<string, Product | undefined>();
    productIds.forEach((id, index) => {
      map.set(id, productQueries[index]?.data);
    });
    return map;
  }, [productIds, productQueries]);

  const rows = useMemo(() => {
    const byVariant = new Map<string, LevelRow>();
    for (const level of rawLevels) {
      const product = productsById.get(level.productId);
      const variant = product?.variants?.find((v) => v.id === level.variantId);
      const row = byVariant.get(level.variantId) ?? {
        variantId: level.variantId,
        productId: level.productId,
        productName: product?.name ?? "…",
        sku: variant?.sku ?? null,
        attributesLabel: variant
          ? Object.entries(variant.attributes)
              .map(([key, val]) => `${key}: ${val}`)
              .join(", ")
          : "",
        qtyByLocation: {},
      };
      row.qtyByLocation[level.locationId] = level.qty;
      byVariant.set(level.variantId, row);
    }
    return Array.from(byVariant.values());
  }, [rawLevels, productsById]);

  function handleReset() {
    setRawQuery("");
    onSearchChange({});
  }

  const columns: ColumnsType<LevelRow> = [
    { title: t("stock.levels.columns.product"), dataIndex: "productName" },
    {
      title: t("stock.levels.columns.variant"),
      dataIndex: "attributesLabel",
      render: (label: string) => label || "—",
    },
    {
      title: t("stock.levels.columns.sku"),
      dataIndex: "sku",
      render: (sku: string | null) => sku ?? "—",
    },
    ...(locations ?? []).map((location): ColumnsType<LevelRow>[number] => ({
      title: location.name,
      key: location.id,
      render: (_, row) => formatQty(row.qtyByLocation[location.id]),
    })),
    {
      title: t("stock.levels.columns.total"),
      key: "total",
      render: (_, row) => formatQty(sumQty(Object.values(row.qtyByLocation))),
    },
  ];

  return (
    <Card
      title={t("stock.levels.title")}
      extra={
        canWrite && (
          <Space>
            <Button onClick={() => setAdjustOpen(true)}>{t("stock.actions.adjust")}</Button>
            <Button onClick={() => setTransferOpen(true)}>{t("stock.actions.transfer")}</Button>
          </Space>
        )
      }
    >
      {/* One row per variant (D-124's result count line below counts these
          rows, not the raw per-location `GET /stock/levels` items). */}
      <FilterBar onReset={handleReset} resultCount={rows.length} hasMore={hasNextPage}>
        <FilterBar.Field label={t("stock.levels.columns.product")}>
          {(labelId) => (
            <Select
              allowClear
              showSearch
              aria-labelledby={labelId}
              placeholder={t("stock.levels.productPlaceholder")}
              style={{ width: "100%" }}
              value={search.productId}
              filterOption={false}
              onSearch={setRawQuery}
              onChange={(value: string | undefined) =>
                onSearchChange({ ...search, productId: value })
              }
              onClear={() => onSearchChange({ ...search, productId: undefined })}
              options={combinedProductOptions}
            />
          )}
        </FilterBar.Field>
        <FilterBar.Field label={t("stock.fields.location")}>
          {(labelId) => (
            <Select
              allowClear
              aria-labelledby={labelId}
              placeholder={t("stock.levels.locationPlaceholder")}
              style={{ width: "100%" }}
              value={search.locationId}
              onChange={(value: string | undefined) =>
                onSearchChange({ ...search, locationId: value })
              }
              onClear={() => onSearchChange({ ...search, locationId: undefined })}
              options={(locations ?? []).map((location) => ({
                value: location.id,
                label: location.name,
              }))}
            />
          )}
        </FilterBar.Field>
      </FilterBar>

      <Table<LevelRow>
        rowKey="variantId"
        columns={columns}
        dataSource={rows}
        loading={isPending}
        pagination={false}
      />
      {hasNextPage && (
        <div style={{ textAlign: "center", marginTop: 16 }}>
          <Button loading={isFetchingNextPage} onClick={() => fetchNextPage()}>
            {t("common.loadMore")}
          </Button>
        </div>
      )}

      {canWrite && (
        <>
          <StockAdjustmentDrawer open={adjustOpen} onClose={() => setAdjustOpen(false)} />
          <StockTransferDrawer open={transferOpen} onClose={() => setTransferOpen(false)} />
        </>
      )}
    </Card>
  );
}
