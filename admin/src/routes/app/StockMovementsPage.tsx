import { useQuery } from "@tanstack/react-query";
import { Button, Card, DatePicker, Select, Table, Tag } from "antd";
import type { ColumnsType } from "antd/es/table";
import dayjs, { type Dayjs } from "dayjs";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";

import { FilterBar } from "../../components/FilterBar";
import { buildDateRangePresets } from "../../lib/dateRangePresets";
import { useCursorList } from "../../lib/useCursorList";
import {
  fetchAllLocations,
  fetchStockMovementsPage,
  type StockMovement,
  type StockMovementKind,
} from "../../stock/api";
import { StockVariantPicker, type StockVariantValue } from "./StockVariantPicker";
import type { StockMovementsSearch } from "./stockMovementsRoute";

export const MOVEMENT_KINDS: StockMovementKind[] = [
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

export interface StockMovementsPageProps {
  /** Validated filter state from the route's search params
   * (`stockMovementsRoute`'s `validateSearch`). */
  search: StockMovementsSearch;
  /** Replaces the filter state — the caller (`stockMovementsRoute`) turns
   * this into a `navigate({ search, replace: true })` call so reload and
   * share restore it, while Back leaves the page instead of undoing one
   * filter at a time (D-124). */
  onSearchChange: (next: StockMovementsSearch) => void;
}

/**
 * The append-only stock ledger (manager+, `stock.write` — no separate
 * `stock.read` permission exists for this narrower manager+ view,
 * `api/internal/auth/permissions.go`). `createdBy` is a raw user id — a
 * manager has no `staff.manage` (owner-only) to resolve it to a name, and
 * `StockMovement` carries no embedded name (checked in `schema.d.ts`), so
 * it renders as-is.
 *
 * Filters — including the variant picker's product/variant selection —
 * live entirely in the route's search params (`search`/`onSearchChange`,
 * D-124); there is no separate local UI state to keep in sync with the URL.
 */
export function StockMovementsPage({ search, onSearchChange }: StockMovementsPageProps) {
  const { t } = useTranslation();

  const variant: StockVariantValue = {
    productId: search.productId ?? null,
    variantId: search.variantId ?? null,
  };

  const { data: locations } = useQuery({
    queryKey: ["locations", "all"],
    queryFn: fetchAllLocations,
  });

  const dateRangeValue = useMemo<[Dayjs | null, Dayjs | null] | null>(() => {
    if (!search.from && !search.to) {
      return null;
    }
    return [search.from ? dayjs(search.from) : null, search.to ? dayjs(search.to) : null];
  }, [search.from, search.to]);

  const datePresets = useMemo(() => buildDateRangePresets(t), [t]);

  const filters = useMemo(
    () => ({
      variantId: search.variantId,
      locationId: search.locationId,
      kind: search.kind,
      from: search.from ? dayjs(search.from).startOf("day").toISOString() : undefined,
      to: search.to ? dayjs(search.to).endOf("day").toISOString() : undefined,
    }),
    [search.variantId, search.locationId, search.kind, search.from, search.to],
  );

  const { data, fetchNextPage, hasNextPage, isFetchingNextPage, isPending } = useCursorList(
    ["stock", "movements", filters],
    (cursor) => fetchStockMovementsPage(filters, cursor),
  );
  const movements = data?.pages.flatMap((page) => page.items) ?? [];

  function handleVariantChange(next: StockVariantValue) {
    onSearchChange({
      ...search,
      productId: next.productId ?? undefined,
      variantId: next.variantId ?? undefined,
    });
  }

  function handleDateRangeChange(value: [Dayjs | null, Dayjs | null] | null) {
    onSearchChange({
      ...search,
      from: value?.[0] ? value[0].format("YYYY-MM-DD") : undefined,
      to: value?.[1] ? value[1].format("YYYY-MM-DD") : undefined,
    });
  }

  function handleReset() {
    onSearchChange({});
  }

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
      <FilterBar onReset={handleReset} resultCount={movements.length} hasMore={hasNextPage}>
        <FilterBar.Field label={t("stock.fields.variant")} span={{ xl: 12 }}>
          {(labelId) => (
            <StockVariantPicker value={variant} onChange={handleVariantChange} labelId={labelId} />
          )}
        </FilterBar.Field>
        <FilterBar.Field label={t("stock.fields.location")}>
          {(labelId) => (
            <Select
              allowClear
              aria-labelledby={labelId}
              placeholder={t("stock.movements.locationPlaceholder")}
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
        <FilterBar.Field label={t("stock.movements.columns.kind")}>
          {(labelId) => (
            <Select
              allowClear
              aria-labelledby={labelId}
              placeholder={t("stock.movements.kindPlaceholder")}
              style={{ width: "100%" }}
              value={search.kind}
              onChange={(value: StockMovementKind | undefined) =>
                onSearchChange({ ...search, kind: value })
              }
              onClear={() => onSearchChange({ ...search, kind: undefined })}
              options={MOVEMENT_KINDS.map((value) => ({
                value,
                label: t(`stock.movementKinds.${value}`),
              }))}
            />
          )}
        </FilterBar.Field>
        <FilterBar.Field label={t("stock.movements.dateRangeLabel")}>
          {(labelId) => (
            <DatePicker.RangePicker
              aria-labelledby={labelId}
              style={{ width: "100%" }}
              value={dateRangeValue}
              presets={datePresets}
              onChange={handleDateRangeChange}
            />
          )}
        </FilterBar.Field>
      </FilterBar>

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
