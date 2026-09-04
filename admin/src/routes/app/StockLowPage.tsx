import { useQueries } from "@tanstack/react-query";
import { Button, Card, Table } from "antd";
import type { ColumnsType } from "antd/es/table";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";

import { fetchProduct, type Product } from "../../catalog/api";
import { useCursorList } from "../../lib/useCursorList";
import { fetchLowStockPage, formatQty, type StockLowItem } from "../../stock/api";

interface LowRow extends StockLowItem {
  productName: string;
  sku: string | null;
  attributesLabel: string;
}

/** Variants at or below their effective low-stock threshold (D-44,
 * manager+). `StockLowItem` carries only ids and numbers (checked in
 * `schema.d.ts`) — product name, variant SKU and attributes are joined
 * client-side the same way as `StockLevelsPage`. */
export function StockLowPage() {
  const { t } = useTranslation();

  const { data, fetchNextPage, hasNextPage, isFetchingNextPage, isPending } = useCursorList(
    ["stock", "low"],
    fetchLowStockPage,
  );
  const items = useMemo(() => data?.pages.flatMap((page) => page.items) ?? [], [data]);

  const productIds = useMemo(
    () => Array.from(new Set(items.map((item) => item.productId))),
    [items],
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

  const rows: LowRow[] = useMemo(
    () =>
      items.map((item) => {
        const product = productsById.get(item.productId);
        const variant = product?.variants?.find((v) => v.id === item.variantId);
        return {
          ...item,
          productName: product?.name ?? "…",
          sku: variant?.sku ?? null,
          attributesLabel: variant
            ? Object.entries(variant.attributes)
                .map(([key, val]) => `${key}: ${val}`)
                .join(", ")
            : "",
        };
      }),
    [items, productsById],
  );

  const columns: ColumnsType<LowRow> = [
    { title: t("stock.low.columns.product"), dataIndex: "productName" },
    {
      title: t("stock.low.columns.variant"),
      dataIndex: "attributesLabel",
      render: (label: string) => label || "—",
    },
    {
      title: t("stock.low.columns.sku"),
      dataIndex: "sku",
      render: (sku: string | null) => sku ?? "—",
    },
    {
      title: t("stock.low.columns.qty"),
      dataIndex: "qty",
      render: (qty: string) => formatQty(qty),
    },
    { title: t("stock.low.columns.threshold"), dataIndex: "threshold" },
  ];

  return (
    <Card title={t("stock.low.title")}>
      <Table<LowRow>
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
    </Card>
  );
}
