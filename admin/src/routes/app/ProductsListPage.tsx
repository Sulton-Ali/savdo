import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Button, Card, Input, Switch, Table, Tag, TreeSelect } from "antd";
import type { ColumnsType } from "antd/es/table";
import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import { useAuth } from "../../auth/AuthContext";
import { fetchCategories, fetchProductsPage, type Product } from "../../catalog/api";
import { buildCategoryTreeSelectData } from "../../catalog/tree";
import { FilterBar } from "../../components/FilterBar";
import { useCursorList } from "../../lib/useCursorList";
import { useDebouncedValue } from "../../lib/useDebouncedValue";
import type { ProductsSearch } from "./productsRoute";

/** Search fires only once the query is empty (clears the filter) or at
 * least 2 characters long (`docs/05-API.md` § Catalogue / T6a spec). */
const MIN_QUERY_LENGTH = 2;
const SEARCH_DEBOUNCE_MS = 300;

export interface ProductsListPageProps {
  /** Validated filter state from the route's search params
   * (`productsRoute`'s `validateSearch`). */
  search: ProductsSearch;
  /** Replaces the filter state — the caller (`productsRoute`) turns this
   * into a `navigate({ search, replace: true })` call so reload and share
   * restore it, while Back leaves the page instead of undoing the search
   * one keystroke at a time (D-124). */
  onSearchChange: (next: ProductsSearch) => void;
}

export function ProductsListPage({ search, onSearchChange }: ProductsListPageProps) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { can } = useAuth();
  const canWrite = can("catalog.write");

  // The input stays local state for typing responsiveness; only the
  // debounced value is pushed into the route's search params. `lastPushedQ`
  // tells the sync-from-url effect below an external change (Reset, browser
  // Back, reload) apart from the round-trip of our own push, so it does not
  // clobber what the user is still typing (mirrors `CustomersPage`).
  const [rawQuery, setRawQuery] = useState(search.q ?? "");
  const debouncedQuery = useDebouncedValue(rawQuery, SEARCH_DEBOUNCE_MS);
  const lastPushedQ = useRef(search.q);

  useEffect(() => {
    if (search.q === lastPushedQ.current) {
      return;
    }
    // A debounce is still pending (the user is mid-typing): let it finish
    // and push its own value instead of clobbering their keystrokes with
    // this external change (Back/Forward, another navigation). Once the
    // debounce settles, the push effect below runs and either matches this
    // external `q` (nothing left to sync) or overwrites it with what the
    // user typed — "last user action wins".
    if (rawQuery !== debouncedQuery) {
      return;
    }
    lastPushedQ.current = search.q;
    setRawQuery(search.q ?? "");
  }, [search.q, rawQuery, debouncedQuery]);

  useEffect(() => {
    const trimmed = debouncedQuery.trim();
    if (trimmed.length > 0 && trimmed.length < MIN_QUERY_LENGTH) {
      return;
    }
    const next = trimmed.length > 0 ? trimmed : undefined;
    if (next === search.q) {
      return;
    }
    lastPushedQ.current = next;
    onSearchChange({ ...search, q: next });
  }, [debouncedQuery, search, onSearchChange]);

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

  // Mirrors the debounce gate above: a URL `q` shorter than
  // `MIN_QUERY_LENGTH` (bookmark, edited address bar, old history entry)
  // still renders in the input via the sync effect, but must not reach the
  // API — the typed path never sends a 1-char query either.
  const filters = {
    q: search.q && search.q.length >= MIN_QUERY_LENGTH ? search.q : undefined,
    categoryId: search.categoryId,
    // A user without catalog.write must never forward includeInactive to
    // the API, even if it is sitting in the URL (e.g. a shared link from a
    // manager) — this page hides the Switch for them, but the URL itself is
    // not a trusted source of permission.
    includeInactive: canWrite ? search.includeInactive : undefined,
  };

  const { data, fetchNextPage, hasNextPage, isFetchingNextPage, isPending } = useCursorList(
    ["products", filters],
    (cursor) => fetchProductsPage(filters, cursor),
  );
  const products = data?.pages.flatMap((page) => page.items) ?? [];
  // `costPrice` is present only for owner/manager responses (ADR-010) — the
  // column follows the actual response shape, never a role assumption.
  const showCostColumn = products.some((product) => product.costPrice !== undefined);

  function handleReset() {
    setRawQuery("");
    lastPushedQ.current = undefined;
    onSearchChange({});
  }

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
      <FilterBar onReset={handleReset} resultCount={products.length} hasMore={hasNextPage}>
        <FilterBar.Field label={t("common.search")}>
          {(labelId) => (
            <Input
              allowClear
              aria-labelledby={labelId}
              placeholder={t("catalog.products.searchPlaceholder")}
              value={rawQuery}
              onChange={(event) => setRawQuery(event.target.value)}
            />
          )}
        </FilterBar.Field>
        <FilterBar.Field label={t("catalog.products.columns.category")}>
          {(labelId) => (
            <TreeSelect
              allowClear
              aria-labelledby={labelId}
              treeData={categoryOptions}
              value={search.categoryId}
              onChange={(value: string | undefined) =>
                onSearchChange({ ...search, categoryId: value })
              }
              placeholder={t("catalog.products.allCategories")}
              style={{ width: "100%" }}
              treeDefaultExpandAll
            />
          )}
        </FilterBar.Field>
        {canWrite && (
          <FilterBar.Field label={t("common.showInactive")} span={{ xs: 12, md: 6, xl: 4 }}>
            {(labelId) => (
              <Switch
                aria-labelledby={labelId}
                checked={search.includeInactive ?? false}
                onChange={(checked) =>
                  onSearchChange({ ...search, includeInactive: checked ? true : undefined })
                }
              />
            )}
          </FilterBar.Field>
        )}
      </FilterBar>

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
