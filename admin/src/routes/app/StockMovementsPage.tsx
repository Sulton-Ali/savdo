import { useQuery } from "@tanstack/react-query";
import { Button, Card, DatePicker, Select, Space, Table, Tag } from "antd";
import type { ColumnsType } from "antd/es/table";
import dayjs from "dayjs";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";

import { useCursorList } from "../../lib/useCursorList";
import {
  fetchAllLocations,
  fetchStockMovementsPage,
  type StockMovement,
  type StockMovementKind,
} from "../../stock/api";
import { StockVariantPicker, type StockVariantValue } from "./StockVariantPicker";

const MOVEMENT_KINDS: StockMovementKind[] = [
  "purchase_in",
  "sale_out",
  "sale_void_in",
  "return_in",
  "adjustment",
  "transfer_out",
  "transfer_in",
];

const KIND_COLORS: Record<StockMovementKind, string> = {
  purchase_in: "green",
  sale_out: "volcano",
  sale_void_in: "blue",
  return_in: "cyan",
  adjustment: "gold",
  transfer_out: "orange",
  transfer_in: "purple",
};

const EMPTY_VARIANT: StockVariantValue = { productId: null, variantId: null };

function formatQtySigned(qty: string): { text: string; color: string } {
  const num = Number(qty);
  if (num > 0) {
    return { text: `+${qty}`, color: "green" };
  }
  if (num < 0) {
    return { text: qty, color: "red" };
  }
  return { text: qty, color: "default" };
}

/**
 * The append-only stock ledger (manager+, `stock.write` — no separate
 * `stock.read` permission exists for this narrower manager+ view,
 * `api/internal/auth/permissions.go`). `createdBy` is a raw user id — a
 * manager has no `staff.manage` (owner-only) to resolve it to a name, and
 * `StockMovement` carries no embedded name (checked in `schema.d.ts`), so
 * it renders as-is.
 */
export function StockMovementsPage() {
  const { t } = useTranslation();

  const [variant, setVariant] = useState<StockVariantValue>(EMPTY_VARIANT);
  const [locationId, setLocationId] = useState<string | undefined>(undefined);
  const [kind, setKind] = useState<StockMovementKind | undefined>(undefined);
  const [dateRange, setDateRange] = useState<[dayjs.Dayjs | null, dayjs.Dayjs | null] | null>(null);

  const { data: locations } = useQuery({
    queryKey: ["locations", "all"],
    queryFn: fetchAllLocations,
  });

  const filters = useMemo(
    () => ({
      variantId: variant.variantId ?? undefined,
      locationId,
      kind,
      from: dateRange?.[0] ? dateRange[0].startOf("day").toISOString() : undefined,
      to: dateRange?.[1] ? dateRange[1].endOf("day").toISOString() : undefined,
    }),
    [variant.variantId, locationId, kind, dateRange],
  );

  const { data, fetchNextPage, hasNextPage, isFetchingNextPage, isPending } = useCursorList(
    ["stock", "movements", filters],
    (cursor) => fetchStockMovementsPage(filters, cursor),
  );
  const movements = data?.pages.flatMap((page) => page.items) ?? [];

  const columns: ColumnsType<StockMovement> = [
    {
      title: t("stock.movements.columns.kind"),
      dataIndex: "kind",
      render: (value: StockMovementKind) => (
        <Tag color={KIND_COLORS[value]}>{t(`stock.movementKinds.${value}`)}</Tag>
      ),
    },
    {
      title: t("stock.movements.columns.qty"),
      dataIndex: "qty",
      render: (qty: string) => {
        const { text, color } = formatQtySigned(qty);
        return <Tag color={color}>{text}</Tag>;
      },
    },
    {
      title: t("stock.movements.columns.unitCost"),
      dataIndex: "unitCost",
      render: (value: string | null) => value ?? "—",
    },
    {
      title: t("stock.movements.columns.reference"),
      key: "reference",
      render: (_, row) =>
        row.refType && row.refId ? `${row.refType} #${row.refId.slice(0, 8)}` : "—",
    },
    {
      title: t("stock.movements.columns.reason"),
      key: "reason",
      render: (_, row) =>
        row.reason ? (
          <span>
            {t(`stock.adjustmentReasons.${row.reason}`, { defaultValue: row.reason })}
            {row.note ? ` — ${row.note}` : ""}
          </span>
        ) : (
          "—"
        ),
    },
    {
      title: t("stock.movements.columns.who"),
      dataIndex: "createdBy",
      render: (value: string | null) => value ?? "—",
    },
    {
      title: t("stock.movements.columns.when"),
      dataIndex: "createdAt",
      render: (value: string) => dayjs(value).format("YYYY-MM-DD HH:mm"),
    },
  ];

  return (
    <Card title={t("stock.movements.title")}>
      <Space style={{ marginBottom: 16, width: "100%" }} direction="vertical">
        <Space wrap align="start">
          <div style={{ width: 320 }}>
            <StockVariantPicker value={variant} onChange={setVariant} />
          </div>
          <Select
            allowClear
            aria-label={t("stock.movements.locationPlaceholder")}
            placeholder={t("stock.movements.locationPlaceholder")}
            style={{ width: 180 }}
            value={locationId}
            onChange={(value: string | undefined) => setLocationId(value)}
            onClear={() => setLocationId(undefined)}
            options={(locations ?? []).map((location) => ({
              value: location.id,
              label: location.name,
            }))}
          />
          <Select
            allowClear
            aria-label={t("stock.movements.kindPlaceholder")}
            placeholder={t("stock.movements.kindPlaceholder")}
            style={{ width: 180 }}
            value={kind}
            onChange={(value: StockMovementKind | undefined) => setKind(value)}
            onClear={() => setKind(undefined)}
            options={MOVEMENT_KINDS.map((value) => ({
              value,
              label: t(`stock.movementKinds.${value}`),
            }))}
          />
          <DatePicker.RangePicker
            aria-label={t("stock.movements.dateRangeLabel")}
            value={dateRange}
            onChange={(value) => setDateRange(value)}
          />
        </Space>
      </Space>

      <Table<StockMovement>
        rowKey="id"
        columns={columns}
        dataSource={movements}
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
