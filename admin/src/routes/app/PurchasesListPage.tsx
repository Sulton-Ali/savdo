import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Button, Card, Select, Table, Tag } from "antd";
import type { ColumnsType } from "antd/es/table";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";

import { FilterBar } from "../../components/FilterBar";
import { useCursorList } from "../../lib/useCursorList";
import { fetchLocationsPage } from "../../locations/api";
import { fetchPurchasesPage, type Purchase, type PurchaseStatus } from "../../purchases/api";
import { fetchSuppliersPage } from "../../suppliers/api";
import type { PurchasesSearch } from "./purchasesRoute";

export const PURCHASE_STATUSES: PurchaseStatus[] = ["draft", "received", "cancelled"];

const STATUS_COLORS: Record<PurchaseStatus, string> = {
  draft: "default",
  received: "green",
  cancelled: "red",
};

export interface PurchasesListPageProps {
  /** Validated filter state from the route's search params
   * (`purchasesRoute`'s `validateSearch`). */
  search: PurchasesSearch;
  /** Replaces the filter state — the caller (`purchasesRoute`) turns this
   * into a `navigate({ search, replace: true })` call so reload and share
   * restore it, while Back leaves the page instead of undoing one filter
   * at a time (D-124). */
  onSearchChange: (next: PurchasesSearch) => void;
}

export function PurchasesListPage({ search, onSearchChange }: PurchasesListPageProps) {
  const { t } = useTranslation();
  const navigate = useNavigate();

  // Not cursor-paginated on this screen — a small shop's supplier/location
  // lists are short, same approach as `ProductsListPage`'s category filter.
  const { data: suppliers } = useQuery({
    queryKey: ["suppliers-options"],
    queryFn: () => fetchSuppliersPage({}, null),
  });
  const { data: locations } = useQuery({
    queryKey: ["locations-options"],
    queryFn: () => fetchLocationsPage(null),
  });
  const suppliersById = useMemo(
    () => new Map((suppliers?.items ?? []).map((supplier) => [supplier.id, supplier])),
    [suppliers],
  );
  const locationsById = useMemo(
    () => new Map((locations?.items ?? []).map((location) => [location.id, location])),
    [locations],
  );

  const filters = { status: search.status, supplierId: search.supplierId };

  const { data, fetchNextPage, hasNextPage, isFetchingNextPage, isPending } = useCursorList(
    ["purchases", filters],
    (cursor) => fetchPurchasesPage(filters, cursor),
  );
  const purchases = data?.pages.flatMap((page) => page.items) ?? [];

  function handleReset() {
    onSearchChange({});
  }

  const columns: ColumnsType<Purchase> = [
    { title: t("purchases.columns.number"), dataIndex: "number" },
    {
      title: t("purchases.columns.supplier"),
      key: "supplier",
      render: (_, row) => suppliersById.get(row.supplierId)?.name ?? "—",
    },
    {
      title: t("purchases.columns.location"),
      key: "location",
      render: (_, row) => locationsById.get(row.locationId)?.name ?? "—",
    },
    {
      title: t("purchases.columns.status"),
      dataIndex: "status",
      render: (value: PurchaseStatus) => (
        <Tag color={STATUS_COLORS[value]}>{t(`purchases.status.${value}`)}</Tag>
      ),
    },
    { title: t("purchases.columns.totalCost"), dataIndex: "totalCost" },
    {
      title: t("purchases.columns.receivedAt"),
      dataIndex: "receivedAt",
      render: (value: string | null) => value ?? "—",
    },
  ];

  return (
    <Card
      title={t("purchases.title")}
      extra={
        <Button type="primary" onClick={() => navigate({ to: "/purchases/new" })}>
          {t("purchases.add")}
        </Button>
      }
    >
      <FilterBar onReset={handleReset} resultCount={purchases.length} hasMore={hasNextPage}>
        <FilterBar.Field label={t("purchases.columns.status")}>
          {(labelId) => (
            <Select
              allowClear
              aria-labelledby={labelId}
              placeholder={t("purchases.allStatuses")}
              style={{ width: "100%" }}
              value={search.status}
              onChange={(value: PurchaseStatus | undefined) =>
                onSearchChange({ ...search, status: value })
              }
              onClear={() => onSearchChange({ ...search, status: undefined })}
              options={PURCHASE_STATUSES.map((value) => ({
                value,
                label: t(`purchases.status.${value}`),
              }))}
            />
          )}
        </FilterBar.Field>
        <FilterBar.Field label={t("purchases.columns.supplier")}>
          {(labelId) => (
            <Select
              allowClear
              showSearch
              aria-labelledby={labelId}
              placeholder={t("purchases.allSuppliers")}
              style={{ width: "100%" }}
              value={search.supplierId}
              onChange={(value: string | undefined) =>
                onSearchChange({ ...search, supplierId: value })
              }
              onClear={() => onSearchChange({ ...search, supplierId: undefined })}
              optionFilterProp="label"
              options={(suppliers?.items ?? []).map((supplier) => ({
                value: supplier.id,
                label: supplier.name,
              }))}
            />
          )}
        </FilterBar.Field>
      </FilterBar>

      <Table<Purchase>
        rowKey="id"
        columns={columns}
        dataSource={purchases}
        loading={isPending}
        pagination={false}
        onRow={(row) => ({
          onClick: () => navigate({ to: "/purchases/$id", params: { id: row.id } }),
          style: { cursor: "pointer" },
        })}
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
