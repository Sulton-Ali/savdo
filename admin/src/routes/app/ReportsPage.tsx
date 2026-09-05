import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import {
  Button,
  Card,
  Descriptions,
  Segmented,
  Select,
  Skeleton,
  Space,
  Table,
  Typography,
} from "antd";
import type { ColumnsType } from "antd/es/table";
import dayjs from "dayjs";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";

import { useAuth } from "../../auth/AuthContext";
import { formatMoney, parseMoney } from "../../lib/money";
import { useCursorList } from "../../lib/useCursorList";
import {
  fetchSalesByProductPage,
  fetchSalesSummaryReport,
  type SalesByProductRow,
  type SalesSummaryReport,
} from "../../reports/api";
import { fetchAllLocations, fetchLowStockPage, formatQty } from "../../stock/api";

type Preset = "today" | "last7Days" | "thisMonth";

/** Today's date range presets (`docs/05-API.md` § Conventions: `from`/`to`
 * are `YYYY-MM-DD`, inclusive, in the shop's timezone — this uses the
 * browser's local date, same as every other date-only picker in this app;
 * the server is the one that applies the shop timezone). "This month" caps
 * at today rather than the month's end since there is no future data to
 * report on. */
function presetRange(preset: Preset): { from: string; to: string } {
  const today = dayjs();
  const to = today.format("YYYY-MM-DD");
  switch (preset) {
    case "today":
      return { from: to, to };
    case "last7Days":
      return { from: today.subtract(6, "day").format("YYYY-MM-DD"), to };
    case "thisMonth":
      return { from: today.startOf("month").format("YYYY-MM-DD"), to };
  }
}

function MoneyValue({ value }: { value: string | undefined }) {
  if (value == null) {
    return <>{"—"}</>;
  }
  return <>{formatMoney(parseMoney(value)) ?? "—"}</>;
}

function SummaryCard({
  data,
  title,
  isPending,
}: {
  data: SalesSummaryReport | undefined;
  title: string;
  isPending: boolean;
}) {
  const { t } = useTranslation();

  if (isPending || !data) {
    return (
      <Card title={title}>
        <Skeleton active />
      </Card>
    );
  }

  return (
    <Card title={title}>
      <Descriptions column={2} bordered size="small">
        <Descriptions.Item label={t("reports.summary.fields.range")}>
          {data.from} — {data.to}
        </Descriptions.Item>
        <Descriptions.Item label={t("reports.summary.fields.salesCount")}>
          {data.salesCount}
        </Descriptions.Item>
        <Descriptions.Item label={t("reports.summary.fields.returnsCount")}>
          {data.returnsCount}
        </Descriptions.Item>
        <Descriptions.Item label={t("reports.summary.fields.revenue")}>
          <MoneyValue value={data.revenue} />
        </Descriptions.Item>
        <Descriptions.Item label={t("reports.summary.fields.discounts")}>
          <MoneyValue value={data.discounts} />
        </Descriptions.Item>
        <Descriptions.Item label={t("reports.summary.fields.refunds")}>
          <MoneyValue value={data.refunds} />
        </Descriptions.Item>
        <Descriptions.Item label={t("reports.summary.fields.netRevenue")}>
          <MoneyValue value={data.netRevenue} />
        </Descriptions.Item>
        {data.cost != null && (
          <Descriptions.Item label={t("reports.summary.fields.cost")}>
            <MoneyValue value={data.cost} />
          </Descriptions.Item>
        )}
        {data.margin != null && (
          <Descriptions.Item label={t("reports.summary.fields.margin")}>
            <MoneyValue value={data.margin} />
          </Descriptions.Item>
        )}
      </Descriptions>
    </Card>
  );
}

function LowStockCard() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { data, isPending } = useQuery({
    queryKey: ["stock", "low", "reportsCard"],
    queryFn: () => fetchLowStockPage(null),
  });

  return (
    <Card title={t("reports.lowStock.title")}>
      <Space direction="vertical">
        {isPending ? (
          <Skeleton.Input active size="small" />
        ) : (
          data && (
            <Typography.Text>
              {t("reports.lowStock.count", {
                value: `${data.items.length}${data.nextCursor ? "+" : ""}`,
              })}
            </Typography.Text>
          )
        )}
        <Button onClick={() => navigate({ to: "/stock/low" })}>{t("reports.lowStock.link")}</Button>
      </Space>
    </Card>
  );
}

/** Sales-by-product table (manager+ only, `reports.read`): server-sorted by
 * revenue descending, cursor-paginated. `cost`/`margin` columns only render
 * when the response actually carries them (ADR-010) — never assumed. */
function ByProductTable({
  from,
  to,
  locationId,
}: {
  from: string;
  to: string;
  locationId?: string;
}) {
  const { t } = useTranslation();

  const { data, fetchNextPage, hasNextPage, isFetchingNextPage, isPending } = useCursorList(
    ["reports", "byProduct", from, to, locationId],
    (cursor) => fetchSalesByProductPage({ from, to, locationId }, cursor),
  );
  const rows = useMemo(() => data?.pages.flatMap((page) => page.items) ?? [], [data]);
  const hasCost = rows.some((row) => row.cost != null);
  const hasMargin = rows.some((row) => row.margin != null);

  const columns: ColumnsType<SalesByProductRow> = [
    { title: t("reports.byProduct.columns.product"), dataIndex: "productName" },
    {
      title: t("reports.byProduct.columns.qtySold"),
      dataIndex: "qtySold",
      render: (value: string) => formatQty(value),
    },
    {
      title: t("reports.byProduct.columns.qtyReturned"),
      dataIndex: "qtyReturned",
      render: (value: string) => formatQty(value),
    },
    {
      title: t("reports.byProduct.columns.revenue"),
      dataIndex: "revenue",
      render: (value: string) => <MoneyValue value={value} />,
    },
    ...(hasCost
      ? [
          {
            title: t("reports.byProduct.columns.cost"),
            dataIndex: "cost",
            render: (value: string | undefined) => <MoneyValue value={value} />,
          } satisfies ColumnsType<SalesByProductRow>[number],
        ]
      : []),
    ...(hasMargin
      ? [
          {
            title: t("reports.byProduct.columns.margin"),
            dataIndex: "margin",
            render: (value: string | undefined) => <MoneyValue value={value} />,
          } satisfies ColumnsType<SalesByProductRow>[number],
        ]
      : []),
  ];

  return (
    <Card title={t("reports.byProduct.title")}>
      <Table<SalesByProductRow>
        rowKey="productId"
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

/** Manager+ view (`reports.read`): date-range presets, an optional location
 * filter, the sales summary and the sales-by-product table. */
function ManagerReports() {
  const { t } = useTranslation();
  const [preset, setPreset] = useState<Preset>("today");
  const [locationId, setLocationId] = useState<string | undefined>(undefined);
  const { from, to } = useMemo(() => presetRange(preset), [preset]);

  const { data: locations } = useQuery({
    queryKey: ["locations", "all"],
    queryFn: fetchAllLocations,
  });

  const { data: summary, isPending: summaryPending } = useQuery({
    queryKey: ["reports", "summary", from, to, locationId],
    queryFn: () => fetchSalesSummaryReport({ from, to, locationId }),
  });

  return (
    <Space direction="vertical" size="large" style={{ width: "100%" }}>
      <Card title={t("reports.title")}>
        <Space wrap>
          <Segmented
            aria-label={t("reports.presetLabel")}
            value={preset}
            onChange={(value) => setPreset(value as Preset)}
            options={[
              { label: t("reports.presets.today"), value: "today" },
              { label: t("reports.presets.last7Days"), value: "last7Days" },
              { label: t("reports.presets.thisMonth"), value: "thisMonth" },
            ]}
          />
          <Select
            allowClear
            aria-label={t("reports.locationPlaceholder")}
            placeholder={t("reports.locationPlaceholder")}
            style={{ width: 180 }}
            value={locationId}
            onChange={(value: string | undefined) => setLocationId(value)}
            onClear={() => setLocationId(undefined)}
            options={(locations ?? []).map((location) => ({
              value: location.id,
              label: location.name,
            }))}
          />
        </Space>
      </Card>

      <SummaryCard data={summary} title={t("reports.summary.title")} isPending={summaryPending} />

      <LowStockCard />

      <ByProductTable from={from} to={to} locationId={locationId} />
    </Space>
  );
}

/** Cashier "my day" view (`reports.own_day`): the summary only, no
 * date/location controls — the server always scopes it to today and that
 * cashier's own sales and echoes the effective `from`/`to` it used
 * (D-55). No by-product table (manager+ only) and no low-stock card
 * (`stock.write`, manager+ only). */
function OwnDayReports() {
  const { t } = useTranslation();
  const { data, isPending } = useQuery({
    queryKey: ["reports", "summary", "own-day"],
    queryFn: () => fetchSalesSummaryReport(),
  });

  return (
    <Space direction="vertical" size="large" style={{ width: "100%" }}>
      <SummaryCard data={data} title={t("reports.myDay.title")} isPending={isPending} />
    </Space>
  );
}

/**
 * `/reports` (`docs/06-ROADMAP.md` Phase 4, D-55): a manager+ sees the full
 * dashboard (date presets, location filter, summary, sales-by-product, a
 * low-stock shortcut); a cashier sees only their own day's summary in a
 * read-only "my day" mode. `reports.read` decides which — the API is the
 * actual enforcement point (ADR-010): a cashier can only ever reach the
 * narrower endpoint behaviour regardless of what this page renders.
 */
export function ReportsPage() {
  const { can } = useAuth();
  return can("reports.read") ? <ManagerReports /> : <OwnDayReports />;
}
