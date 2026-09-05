import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Button, Card, DatePicker, Select, Space, Table, Tag } from "antd";
import type { ColumnsType } from "antd/es/table";
import dayjs, { type Dayjs } from "dayjs";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";

import { useAuth } from "../../auth/AuthContext";
import { fetchCustomersPage } from "../../customers/api";
import { formatMoney, parseMoney } from "../../lib/money";
import { useCursorList } from "../../lib/useCursorList";
import { fetchLocationsPage } from "../../locations/api";
import {
  fetchSalesPage,
  type SaleKind,
  type SaleListFilters,
  type SaleStatus,
  type SaleSummary,
} from "../../sales/api";
import { fetchStaffPage } from "../../staff/api";

/** Matches `CustomersPage`'s search minimum (`docs/05-API.md` §
 * Conventions: free-text search is ILIKE/trigram). */
const MIN_QUERY_LENGTH = 2;
const SEARCH_DEBOUNCE_MS = 300;

const SALE_KINDS: SaleKind[] = ["sale", "return"];
const SALE_STATUSES: SaleStatus[] = ["completed", "voided"];

const STATUS_COLORS: Record<SaleStatus, string> = {
  completed: "green",
  voided: "red",
};

/** A customer option kept in the filter regardless of the live search
 * results (`labelInValue`, since a debounced search's option list would
 * otherwise drop the previously chosen customer as soon as the query text
 * changes or is cleared). */
interface CustomerOption {
  value: string;
  label: string;
}

/**
 * Browse every sale (`docs/04-DATA-MODEL.md` § 7: `cashier+` may list every
 * sale for the whole shop and any day, not only their own, D-63). Row click
 * opens `SaleDetailPage`. The cashier filter's data source, `GET /staff`, is
 * owner-only (`docs/05-API.md`), so that one filter is only shown to the
 * owner — a manager or cashier still sees and uses every other filter.
 */
export function SalesListPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { isOwner } = useAuth();

  const [dateRange, setDateRange] = useState<[Dayjs | null, Dayjs | null] | null>(null);
  const [locationId, setLocationId] = useState<string | undefined>(undefined);
  const [kind, setKind] = useState<SaleKind | undefined>(undefined);
  const [status, setStatus] = useState<SaleStatus | undefined>(undefined);
  const [cashierId, setCashierId] = useState<string | undefined>(undefined);
  const [customer, setCustomer] = useState<CustomerOption | undefined>(undefined);

  const [customerSearch, setCustomerSearch] = useState("");
  const [debouncedCustomerSearch, setDebouncedCustomerSearch] = useState("");
  useEffect(() => {
    const trimmed = customerSearch.trim();
    if (trimmed.length > 0 && trimmed.length < MIN_QUERY_LENGTH) {
      return;
    }
    const timer = setTimeout(() => setDebouncedCustomerSearch(trimmed), SEARCH_DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [customerSearch]);

  // Not cursor-paginated here — a small shop's location/staff lists are
  // short, same approach as `PurchasesListPage`'s supplier/location filters.
  const { data: locations } = useQuery({
    queryKey: ["locations-options"],
    queryFn: () => fetchLocationsPage(null),
  });
  const { data: staffPage } = useQuery({
    queryKey: ["staff-options"],
    queryFn: () => fetchStaffPage(null),
    enabled: isOwner,
  });
  const { data: customerOptions } = useQuery({
    queryKey: ["sales-customer-options", debouncedCustomerSearch],
    queryFn: () => fetchCustomersPage({ q: debouncedCustomerSearch }, null),
    enabled: debouncedCustomerSearch.length >= MIN_QUERY_LENGTH,
  });

  const filters: SaleListFilters = useMemo(
    () => ({
      from: dateRange?.[0] ? dateRange[0].format("YYYY-MM-DD") : undefined,
      to: dateRange?.[1] ? dateRange[1].format("YYYY-MM-DD") : undefined,
      locationId,
      cashierId,
      customerId: customer?.value,
      kind,
      status,
    }),
    [dateRange, locationId, cashierId, customer, kind, status],
  );

  const { data, fetchNextPage, hasNextPage, isFetchingNextPage, isPending } = useCursorList(
    ["sales", filters],
    (cursor) => fetchSalesPage(filters, cursor),
  );
  const sales = data?.pages.flatMap((page) => page.items) ?? [];

  const columns: ColumnsType<SaleSummary> = [
    { title: t("sales.columns.number"), dataIndex: "number" },
    {
      title: t("sales.columns.date"),
      dataIndex: "completedAt",
      render: (value: string) => dayjs(value).format("YYYY-MM-DD HH:mm"),
    },
    {
      title: t("sales.columns.kind"),
      dataIndex: "kind",
      render: (value: SaleSummary["kind"]) => t(`sales.kind.${value}`),
    },
    {
      title: t("sales.columns.status"),
      dataIndex: "status",
      render: (value: SaleStatus) => (
        <Tag color={STATUS_COLORS[value]}>{t(`sales.status.${value}`)}</Tag>
      ),
    },
    { title: t("sales.columns.location"), dataIndex: "locationName" },
    { title: t("sales.columns.cashier"), dataIndex: "cashierName" },
    {
      title: t("sales.columns.customer"),
      dataIndex: "customerName",
      render: (value: string | null) => value ?? "—",
    },
    {
      title: t("sales.columns.total"),
      dataIndex: "total",
      render: (value: string) => formatMoney(parseMoney(value)) ?? "—",
    },
    {
      title: t("sales.columns.paymentMethod"),
      dataIndex: "paymentMethod",
      render: (value: SaleSummary["paymentMethod"]) => t(`sales.paymentMethod.${value}`),
    },
  ];

  return (
    <Card title={t("sales.listTitle")}>
      <Space style={{ marginBottom: 16 }} wrap align="start">
        <DatePicker.RangePicker
          aria-label={t("sales.filters.dateRangeLabel")}
          value={dateRange}
          onChange={(value) => setDateRange(value)}
        />
        <Select<string | undefined>
          allowClear
          placeholder={t("sales.filters.allLocations")}
          value={locationId}
          onChange={setLocationId}
          style={{ width: 180 }}
          options={(locations?.items ?? []).map((location) => ({
            value: location.id,
            label: location.name,
          }))}
        />
        <Select<SaleKind | undefined>
          allowClear
          placeholder={t("sales.filters.allKinds")}
          value={kind}
          onChange={setKind}
          style={{ width: 150 }}
          options={SALE_KINDS.map((value) => ({ value, label: t(`sales.kind.${value}`) }))}
        />
        <Select<SaleStatus | undefined>
          allowClear
          placeholder={t("sales.filters.allStatuses")}
          value={status}
          onChange={setStatus}
          style={{ width: 150 }}
          options={SALE_STATUSES.map((value) => ({ value, label: t(`sales.status.${value}`) }))}
        />
        {isOwner && (
          <Select<string | undefined>
            allowClear
            showSearch
            placeholder={t("sales.filters.allCashiers")}
            value={cashierId}
            onChange={setCashierId}
            style={{ width: 200 }}
            optionFilterProp="label"
            options={(staffPage?.items ?? []).map((staffMember) => ({
              value: staffMember.id,
              label: staffMember.fullName,
            }))}
          />
        )}
        <Select<CustomerOption | undefined>
          allowClear
          showSearch
          labelInValue
          placeholder={t("sales.filters.customerPlaceholder")}
          value={customer}
          onSearch={setCustomerSearch}
          onChange={(value) => setCustomer(value ?? undefined)}
          filterOption={false}
          style={{ width: 220 }}
          options={(customerOptions?.items ?? []).map((candidate) => ({
            value: candidate.id,
            label: candidate.fullName,
          }))}
        />
      </Space>

      <Table<SaleSummary>
        rowKey="id"
        columns={columns}
        dataSource={sales}
        loading={isPending}
        pagination={false}
        onRow={(row) => ({
          onClick: () => navigate({ to: "/sales/$id", params: { id: row.id } }),
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
