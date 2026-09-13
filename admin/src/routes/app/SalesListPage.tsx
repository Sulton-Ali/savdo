import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Button, Card, DatePicker, Select, Table, Tag } from "antd";
import type { ColumnsType } from "antd/es/table";
import dayjs, { type Dayjs } from "dayjs";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";

import { useAuth } from "../../auth/AuthContext";
import { FilterBar } from "../../components/FilterBar";
import { fetchCustomer, fetchCustomersPage } from "../../customers/api";
import { buildDateRangePresets } from "../../lib/dateRangePresets";
import { formatMoney, parseMoney } from "../../lib/money";
import { useCursorList } from "../../lib/useCursorList";
import { useDebouncedValue } from "../../lib/useDebouncedValue";
import { fetchLocationsPage } from "../../locations/api";
import {
  fetchSalesPage,
  type SaleKind,
  type SaleListFilters,
  type SaleStatus,
  type SaleSummary,
} from "../../sales/api";
import { fetchStaffPage } from "../../staff/api";
import type { SalesListSearch } from "./salesListRoute";

/** Matches `CustomersPage`'s search minimum (`docs/05-API.md` §
 * Conventions: free-text search is ILIKE/trigram). */
const MIN_QUERY_LENGTH = 2;
const SEARCH_DEBOUNCE_MS = 300;

export const SALE_KINDS: SaleKind[] = ["sale", "return"];
export const SALE_STATUSES: SaleStatus[] = ["completed", "voided"];

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

export interface SalesListPageProps {
  /** Validated filter state from the route's search params
   * (`salesListRoute`'s `validateSearch`). */
  search: SalesListSearch;
  /** Replaces the filter state — the caller (`salesListRoute`) turns this
   * into a `navigate({ search, replace: true })` call so reload and share
   * restore it, while Back leaves the page instead of undoing one filter
   * at a time (D-124). */
  onSearchChange: (next: SalesListSearch) => void;
}

/**
 * Browse every sale (`docs/04-DATA-MODEL.md` § 7: `cashier+` may list every
 * sale for the whole shop and any day, not only their own, D-63). Row click
 * opens `SaleDetailPage`. The cashier filter's data source, `GET /staff`, is
 * owner-only (`docs/05-API.md`), so that one filter is only shown to the
 * owner — a manager or cashier still sees and uses every other filter; a
 * stale or hand-crafted `?cashierId=` in the URL is never forwarded to the
 * API for a non-owner caller either (`filters` below).
 *
 * Filters live in the route's search params (`search`/`onSearchChange`,
 * D-124), except the customer search box's live text, which only drives
 * the select's options and is never itself a search param — only the
 * chosen `customerId` is. On reload with a `customerId` already in the URL,
 * the customer's label is unknown until `GET /customers/{id}` resolves it
 * (same idea as `StockVariantPicker`'s product/variant seed from
 * `productId`); if that lookup fails, `customerId` is dropped from the
 * search.
 */
export function SalesListPage({ search, onSearchChange }: SalesListPageProps) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { isOwner } = useAuth();

  const [selectedCustomer, setSelectedCustomer] = useState<CustomerOption | undefined>(undefined);
  const [customerSearchText, setCustomerSearchText] = useState("");
  const debouncedCustomerSearch = useDebouncedValue(customerSearchText, SEARCH_DEBOUNCE_MS);
  const trimmedCustomerSearch = debouncedCustomerSearch.trim();

  // Whether the currently selected customer option (if any) still matches
  // the URL's `customerId` — false right after a reload/browser-Back that
  // lands on a `customerId` this page has not resolved a label for yet.
  const customerNeedsSeed =
    search.customerId != null && selectedCustomer?.value !== search.customerId;

  useEffect(() => {
    if (search.customerId == null) {
      setSelectedCustomer(undefined);
    }
  }, [search.customerId]);

  const { data: seededCustomer, isError: seedCustomerFailed } = useQuery({
    queryKey: ["sales-customer-seed", search.customerId],
    queryFn: () => fetchCustomer(search.customerId as string),
    enabled: customerNeedsSeed,
    retry: false,
  });

  useEffect(() => {
    if (seededCustomer && seededCustomer.id === search.customerId) {
      setSelectedCustomer({ value: seededCustomer.id, label: seededCustomer.fullName });
    }
  }, [seededCustomer, search.customerId]);

  // biome-ignore lint/correctness/useExhaustiveDependencies: only re-run when the seed lookup itself settles — `search`/`onSearchChange` change on every filter tweak, and re-running this on those would re-fire the drop for an id that already failed once.
  useEffect(() => {
    if (customerNeedsSeed && seedCustomerFailed) {
      onSearchChange({ ...search, customerId: undefined });
    }
  }, [customerNeedsSeed, seedCustomerFailed]);

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
    queryKey: ["sales-customer-options", trimmedCustomerSearch],
    queryFn: () => fetchCustomersPage({ q: trimmedCustomerSearch }, null),
    enabled: trimmedCustomerSearch.length >= MIN_QUERY_LENGTH,
  });

  const dateRangeValue = useMemo<[Dayjs | null, Dayjs | null] | null>(() => {
    if (!search.from && !search.to) {
      return null;
    }
    return [search.from ? dayjs(search.from) : null, search.to ? dayjs(search.to) : null];
  }, [search.from, search.to]);

  const datePresets = useMemo(() => buildDateRangePresets(t), [t]);

  const filters: SaleListFilters = useMemo(
    () => ({
      from: search.from,
      to: search.to,
      locationId: search.locationId,
      // `cashierId` is owner-only data (`GET /staff`) — never forwarded
      // for a non-owner, even if the URL carries one.
      cashierId: isOwner ? search.cashierId : undefined,
      customerId: search.customerId,
      kind: search.kind,
      status: search.status,
    }),
    [search, isOwner],
  );

  const { data, fetchNextPage, hasNextPage, isFetchingNextPage, isPending } = useCursorList(
    ["sales", filters],
    (cursor) => fetchSalesPage(filters, cursor),
  );
  const sales = data?.pages.flatMap((page) => page.items) ?? [];

  function handleDateRangeChange(value: [Dayjs | null, Dayjs | null] | null) {
    onSearchChange({
      ...search,
      from: value?.[0] ? value[0].format("YYYY-MM-DD") : undefined,
      to: value?.[1] ? value[1].format("YYYY-MM-DD") : undefined,
    });
  }

  function handleCustomerChange(value: CustomerOption | undefined) {
    setSelectedCustomer(value);
    onSearchChange({ ...search, customerId: value?.value });
  }

  function handleReset() {
    setSelectedCustomer(undefined);
    setCustomerSearchText("");
    onSearchChange({});
  }

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
      <FilterBar onReset={handleReset} resultCount={sales.length} hasMore={hasNextPage}>
        <FilterBar.Field label={t("sales.filters.dateRangeLabel")}>
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
        <FilterBar.Field label={t("sales.fields.location")}>
          {(labelId) => (
            <Select
              allowClear
              aria-labelledby={labelId}
              placeholder={t("sales.filters.allLocations")}
              style={{ width: "100%" }}
              value={search.locationId}
              onChange={(value: string | undefined) =>
                onSearchChange({ ...search, locationId: value })
              }
              onClear={() => onSearchChange({ ...search, locationId: undefined })}
              options={(locations?.items ?? []).map((location) => ({
                value: location.id,
                label: location.name,
              }))}
            />
          )}
        </FilterBar.Field>
        <FilterBar.Field label={t("sales.columns.kind")}>
          {(labelId) => (
            <Select
              allowClear
              aria-labelledby={labelId}
              placeholder={t("sales.filters.allKinds")}
              style={{ width: "100%" }}
              value={search.kind}
              onChange={(value: SaleKind | undefined) => onSearchChange({ ...search, kind: value })}
              onClear={() => onSearchChange({ ...search, kind: undefined })}
              options={SALE_KINDS.map((value) => ({ value, label: t(`sales.kind.${value}`) }))}
            />
          )}
        </FilterBar.Field>
        <FilterBar.Field label={t("sales.columns.status")}>
          {(labelId) => (
            <Select
              allowClear
              aria-labelledby={labelId}
              placeholder={t("sales.filters.allStatuses")}
              style={{ width: "100%" }}
              value={search.status}
              onChange={(value: SaleStatus | undefined) =>
                onSearchChange({ ...search, status: value })
              }
              onClear={() => onSearchChange({ ...search, status: undefined })}
              options={SALE_STATUSES.map((value) => ({ value, label: t(`sales.status.${value}`) }))}
            />
          )}
        </FilterBar.Field>
        {isOwner && (
          <FilterBar.Field label={t("sales.columns.cashier")}>
            {(labelId) => (
              <Select
                allowClear
                showSearch
                aria-labelledby={labelId}
                placeholder={t("sales.filters.allCashiers")}
                style={{ width: "100%" }}
                value={search.cashierId}
                onChange={(value: string | undefined) =>
                  onSearchChange({ ...search, cashierId: value })
                }
                onClear={() => onSearchChange({ ...search, cashierId: undefined })}
                optionFilterProp="label"
                options={(staffPage?.items ?? []).map((staffMember) => ({
                  value: staffMember.id,
                  label: staffMember.fullName,
                }))}
              />
            )}
          </FilterBar.Field>
        )}
        <FilterBar.Field label={t("sales.fields.customer")}>
          {(labelId) => (
            <Select<CustomerOption | undefined>
              allowClear
              showSearch
              labelInValue
              aria-labelledby={labelId}
              placeholder={t("sales.filters.customerPlaceholder")}
              value={selectedCustomer}
              onSearch={setCustomerSearchText}
              onChange={(value) => handleCustomerChange(value ?? undefined)}
              filterOption={false}
              style={{ width: "100%" }}
              options={(customerOptions?.items ?? []).map((candidate) => ({
                value: candidate.id,
                label: candidate.fullName,
              }))}
            />
          )}
        </FilterBar.Field>
      </FilterBar>

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
