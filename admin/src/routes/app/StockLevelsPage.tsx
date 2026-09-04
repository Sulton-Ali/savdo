import { useQueries, useQuery } from "@tanstack/react-query";
import { Button, Card, Select, Space, Table } from "antd";
import type { ColumnsType } from "antd/es/table";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";

import { useAuth } from "../../auth/AuthContext";
import { fetchProduct, fetchProductsPage, type Product } from "../../catalog/api";
import { useCursorList } from "../../lib/useCursorList";
import { fetchAllLocations, fetchStockLevelsPage, formatQty, sumQty } from "../../stock/api";
import { StockAdjustmentDrawer, StockTransferDrawer } from "./StockActionsDrawer";

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

/**
 * Stock levels grid — one row per variant, one column per location, plus a
 * total (D-40: every authenticated role, cashier included, sees exact
 * quantities; no cost anywhere on this page). `GET /stock/levels` carries
 * only `variantId`/`productId`/`locationId`/`qty` (`StockLevel`, checked in
 * `packages/api-client/src/schema.d.ts` before writing this) — product name,
 * variant SKU and attributes are joined client-side from `GET
 * /products/{id}` (always includes `variants` for any role, `catalog/products.go`).
 */
export function StockLevelsPage() {
  const { t } = useTranslation();
  const { can } = useAuth();
  const canWrite = can("stock.write");

  const [rawQuery, setRawQuery] = useState("");
  const [debouncedQuery, setDebouncedQuery] = useState("");
  const [productId, setProductId] = useState<string | undefined>(undefined);
  const [locationId, setLocationId] = useState<string | undefined>(undefined);
  const [adjustOpen, setAdjustOpen] = useState(false);
  const [transferOpen, setTransferOpen] = useState(false);

  useEffect(() => {
    const trimmed = rawQuery.trim();
    if (trimmed.length > 0 && trimmed.length < MIN_QUERY_LENGTH) {
      return;
    }
    const timer = setTimeout(() => setDebouncedQuery(trimmed), SEARCH_DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [rawQuery]);

  const { data: productPage } = useQuery({
    queryKey: ["stock", "productSearch", debouncedQuery],
    queryFn: () => fetchProductsPage({ q: debouncedQuery || undefined }, null),
  });
  const productOptions = (productPage?.items ?? []).map((product) => ({
    value: product.id,
    label: product.name,
  }));

  const { data: locations } = useQuery({
    queryKey: ["locations", "all"],
    queryFn: fetchAllLocations,
  });

  const filters = { productId, locationId };
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
      <Space style={{ marginBottom: 16 }} wrap>
        <Select
          allowClear
          showSearch
          aria-label={t("stock.levels.productPlaceholder")}
          placeholder={t("stock.levels.productPlaceholder")}
          style={{ width: 240 }}
          value={productId}
          filterOption={false}
          onSearch={setRawQuery}
          onChange={(value: string | undefined) => setProductId(value)}
          onClear={() => setProductId(undefined)}
          options={productOptions}
        />
        <Select
          allowClear
          aria-label={t("stock.levels.locationPlaceholder")}
          placeholder={t("stock.levels.locationPlaceholder")}
          style={{ width: 200 }}
          value={locationId}
          onChange={(value: string | undefined) => setLocationId(value)}
          onClear={() => setLocationId(undefined)}
          options={(locations ?? []).map((location) => ({
            value: location.id,
            label: location.name,
          }))}
        />
      </Space>

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
