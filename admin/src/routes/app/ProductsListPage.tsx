import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Button, Card, Input, Space, Switch, Table, Tag, TreeSelect } from "antd";
import type { ColumnsType } from "antd/es/table";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";

import { useAuth } from "../../auth/AuthContext";
import { fetchCategories, fetchProductsPage, type Product } from "../../catalog/api";
import { buildCategoryTreeSelectData } from "../../catalog/tree";
import { useCursorList } from "../../lib/useCursorList";

/** Search fires only once the query is empty (clears the filter) or at
 * least 2 characters long (`docs/05-API.md` § Catalogue / T6a spec). */
const MIN_QUERY_LENGTH = 2;
const SEARCH_DEBOUNCE_MS = 300;

export function ProductsListPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { can } = useAuth();
  const canWrite = can("catalog.write");

  const [rawQuery, setRawQuery] = useState("");
  const [debouncedQuery, setDebouncedQuery] = useState("");
  const [categoryId, setCategoryId] = useState<string | undefined>(undefined);
  const [includeInactive, setIncludeInactive] = useState(false);

  useEffect(() => {
    const trimmed = rawQuery.trim();
    if (trimmed.length > 0 && trimmed.length < MIN_QUERY_LENGTH) {
      return;
    }
    const timer = setTimeout(() => setDebouncedQuery(trimmed), SEARCH_DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [rawQuery]);

  // Only catalog.write sees inactive categories in the filter dropdown — a
  // cashier (no catalog.write) must never request them (T6a review MAJOR 2).
  const { data: categories } = useQuery({
    queryKey: ["categories", canWrite],
    queryFn: () => fetchCategories(canWrite),
  });
  const categoriesById = useMemo(
    () => new Map((categories ?? []).map((category) => [category.id, category])),
    [categories],
  );
  const categoryOptions = useMemo(
    () => buildCategoryTreeSelectData(categories ?? []),
    [categories],
  );

  const filters = {
    q: debouncedQuery || undefined,
    categoryId,
    includeInactive: canWrite ? includeInactive : undefined,
  };

  const { data, fetchNextPage, hasNextPage, isFetchingNextPage, isPending } = useCursorList(
    ["products", filters],
    (cursor) => fetchProductsPage(filters, cursor),
  );
  const products = data?.pages.flatMap((page) => page.items) ?? [];
  // `costPrice` is present only for owner/manager responses (ADR-010) — the
  // column follows the actual response shape, never a role assumption.
  const showCostColumn = products.some((product) => product.costPrice !== undefined);

  const columns: ColumnsType<Product> = [
    {
      title: t("catalog.products.columns.image"),
      key: "image",
      render: (_, row) => {
        const thumb = row.images?.[0]?.urls.thumb;
        return thumb ? (
          // biome-ignore lint/a11y/useAltText: decorative catalogue thumbnail, name column carries the label
          <img src={thumb} width={40} height={40} style={{ objectFit: "cover" }} />
        ) : null;
      },
    },
    { title: t("catalog.products.columns.name"), dataIndex: "name" },
    {
      title: t("catalog.products.columns.sku"),
      dataIndex: "sku",
      render: (sku: string | null) => sku ?? "—",
    },
    {
      title: t("catalog.products.columns.category"),
      key: "category",
      render: (_, row) =>
        row.categoryId
          ? (categoriesById.get(row.categoryId)?.name ?? "—")
          : t("catalog.products.uncategorized"),
    },
    { title: t("catalog.products.columns.basePrice"), dataIndex: "basePrice" },
    ...(showCostColumn
      ? ([
          { title: t("catalog.products.columns.costPrice"), dataIndex: "costPrice" },
        ] satisfies ColumnsType<Product>)
      : []),
    {
      title: t("catalog.products.columns.promo"),
      key: "promo",
      render: (_, row) =>
        row.promoPrice ? <Tag color="gold">{t("catalog.products.columns.promo")}</Tag> : null,
    },
    {
      title: t("catalog.products.columns.featured"),
      key: "featured",
      render: (_, row) =>
        row.isFeatured ? <Tag color="blue">{t("catalog.products.columns.featured")}</Tag> : null,
    },
    {
      title: t("catalog.products.columns.active"),
      dataIndex: "isActive",
      render: (isActive: boolean) => <Switch checked={isActive} disabled />,
    },
    // An explicit action, not just the row's onClick — the row click has no
    // keyboard path (T6a review MAJOR 3).
    ...(canWrite
      ? ([
          {
            title: "",
            key: "actions",
            render: (_: unknown, row: Product) => (
              <Button
                size="small"
                onClick={(event) => {
                  event.stopPropagation();
                  navigate({ to: "/products/$id", params: { id: row.id } });
                }}
              >
                {t("catalog.products.edit")}
              </Button>
            ),
          },
        ] satisfies ColumnsType<Product>)
      : []),
  ];

  return (
    <Card
      title={t("catalog.products.title")}
      extra={
        canWrite && (
          <Button type="primary" onClick={() => navigate({ to: "/products/new" })}>
            {t("catalog.products.add")}
          </Button>
        )
      }
    >
      <Space style={{ marginBottom: 16 }} wrap>
        <Input.Search
          allowClear
          placeholder={t("catalog.products.searchPlaceholder")}
          value={rawQuery}
          onChange={(event) => setRawQuery(event.target.value)}
          style={{ width: 240 }}
        />
        <TreeSelect
          allowClear
          treeData={categoryOptions}
          value={categoryId}
          onChange={(value: string | undefined) => setCategoryId(value)}
          placeholder={t("catalog.products.allCategories")}
          style={{ width: 220 }}
          treeDefaultExpandAll
        />
        {canWrite && (
          <Space>
            <Switch checked={includeInactive} onChange={setIncludeInactive} />
            {t("common.showInactive")}
          </Space>
        )}
      </Space>

      <Table<Product>
        rowKey="id"
        columns={columns}
        dataSource={products}
        loading={isPending}
        pagination={false}
        onRow={(row) =>
          canWrite
            ? {
                onClick: () => navigate({ to: "/products/$id", params: { id: row.id } }),
                style: { cursor: "pointer" },
              }
            : {}
        }
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
