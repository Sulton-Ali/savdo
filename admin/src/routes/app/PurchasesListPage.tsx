import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Button, Card, Select, Space, Table, Tag } from "antd";
import type { ColumnsType } from "antd/es/table";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { useCursorList } from "../../lib/useCursorList";
import { fetchLocationsPage } from "../../locations/api";
import { fetchPurchasesPage, type Purchase, type PurchaseStatus } from "../../purchases/api";
import { fetchSuppliersPage } from "../../suppliers/api";

const PURCHASE_STATUSES: PurchaseStatus[] = ["draft", "received", "cancelled"];

const STATUS_COLORS: Record<PurchaseStatus, string> = {
  draft: "default",
  received: "green",
  cancelled: "red",
};

export function PurchasesListPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();

  const [status, setStatus] = useState<PurchaseStatus | undefined>(undefined);
  const [supplierId, setSupplierId] = useState<string | undefined>(undefined);

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

  const filters = { status, supplierId };

  const { data, fetchNextPage, hasNextPage, isFetchingNextPage, isPending } = useCursorList(
    ["purchases", filters],
    (cursor) => fetchPurchasesPage(filters, cursor),
  );
  const purchases = data?.pages.flatMap((page) => page.items) ?? [];

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
      <Space style={{ marginBottom: 16 }} wrap>
        <Select<PurchaseStatus | undefined>
          allowClear
          placeholder={t("purchases.allStatuses")}
          value={status}
          onChange={setStatus}
          style={{ width: 180 }}
          options={PURCHASE_STATUSES.map((value) => ({
            value,
            label: t(`purchases.status.${value}`),
          }))}
        />
        <Select<string | undefined>
          allowClear
          showSearch
          placeholder={t("purchases.allSuppliers")}
          value={supplierId}
          onChange={setSupplierId}
          style={{ width: 220 }}
          optionFilterProp="label"
          options={(suppliers?.items ?? []).map((supplier) => ({
            value: supplier.id,
            label: supplier.name,
          }))}
        />
      </Space>

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
