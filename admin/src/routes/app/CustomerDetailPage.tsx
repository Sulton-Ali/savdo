import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Button, Card, Descriptions, Skeleton, Space, Table, Tag, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import dayjs from "dayjs";
import { ArrowLeft } from "lucide-react";
import { useTranslation } from "react-i18next";

import { fetchCustomer, fetchCustomerSalesPage, type SaleSummary } from "../../customers/api";
import { formatMoney, parseMoney } from "../../lib/money";
import { useCursorList } from "../../lib/useCursorList";

const SALE_STATUS_COLORS: Record<SaleSummary["status"], string> = {
  completed: "green",
  voided: "red",
};

/**
 * A customer's details plus their purchase history (`docs/04-DATA-MODEL.md`
 * § 5 CRM). Read-only — editing stays on `CustomersPage`'s drawer, same as
 * every other list/detail pair in this app. `GET /sales?customerId=` is
 * `cashier+` (D-63), same as `GET /customers/{id}`, so this page has no
 * `customers.write`/`cost.read` gating: `SaleSummary` never carries
 * `unitCost` or margin regardless of caller (ADR-010). No sale detail link
 * yet — `number` renders as plain text (that screen is a separate task).
 */
export function CustomerDetailPage({ customerId }: { customerId: string }) {
  const { t } = useTranslation();
  const navigate = useNavigate();

  const {
    data: customer,
    isPending,
    isError,
  } = useQuery({
    queryKey: ["customer", customerId],
    queryFn: () => fetchCustomer(customerId),
  });

  const {
    data: salesData,
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
    isPending: salesPending,
  } = useCursorList(["customer-sales", customerId], (cursor) =>
    fetchCustomerSalesPage(customerId, cursor),
  );
  const sales = salesData?.pages.flatMap((page) => page.items) ?? [];

  const header = (
    <Space>
      <Button
        type="text"
        icon={<ArrowLeft size={16} />}
        onClick={() => navigate({ to: "/customers" })}
      >
        {t("common.back")}
      </Button>
      <Typography.Text strong>{t("customers.detail.title")}</Typography.Text>
    </Space>
  );

  if (isPending) {
    return (
      <Card title={header}>
        <Skeleton active />
      </Card>
    );
  }

  if (isError || !customer) {
    return (
      <Card title={header}>
        <Typography.Text>{t("customers.detail.notFound")}</Typography.Text>
      </Card>
    );
  }

  const columns: ColumnsType<SaleSummary> = [
    { title: t("customers.history.columns.number"), dataIndex: "number" },
    {
      title: t("customers.history.columns.date"),
      dataIndex: "completedAt",
      render: (value: string) => dayjs(value).format("YYYY-MM-DD HH:mm"),
    },
    {
      title: t("customers.history.columns.total"),
      dataIndex: "total",
      render: (value: string) => formatMoney(parseMoney(value)) ?? "—",
    },
    {
      title: t("customers.history.columns.kind"),
      dataIndex: "kind",
      render: (value: SaleSummary["kind"]) => t(`customers.history.kind.${value}`),
    },
    {
      title: t("customers.history.columns.status"),
      dataIndex: "status",
      render: (value: SaleSummary["status"]) => (
        <Tag color={SALE_STATUS_COLORS[value]}>{t(`customers.history.status.${value}`)}</Tag>
      ),
    },
    {
      title: t("customers.history.columns.paymentMethod"),
      dataIndex: "paymentMethod",
      render: (value: SaleSummary["paymentMethod"]) =>
        t(`customers.history.paymentMethod.${value}`),
    },
  ];

  return (
    <Space direction="vertical" size="large" style={{ width: "100%" }}>
      <Card title={header}>
        <Descriptions column={1} bordered size="small">
          <Descriptions.Item label={t("customers.fields.fullName")}>
            {customer.fullName}
          </Descriptions.Item>
          <Descriptions.Item label={t("customers.fields.phone")}>
            {customer.phone ?? "—"}
          </Descriptions.Item>
          <Descriptions.Item label={t("customers.fields.telegramUsername")}>
            {customer.telegramUsername ?? "—"}
          </Descriptions.Item>
          <Descriptions.Item label={t("customers.fields.note")}>
            {customer.note ?? "—"}
          </Descriptions.Item>
          <Descriptions.Item label={t("customers.fields.tags")}>
            {customer.tags.length > 0 ? (
              <Space size={4} wrap>
                {customer.tags.map((tag) => (
                  <Tag key={tag}>{tag}</Tag>
                ))}
              </Space>
            ) : (
              "—"
            )}
          </Descriptions.Item>
        </Descriptions>
      </Card>

      <Card title={t("customers.history.title")}>
        <Table<SaleSummary>
          rowKey="id"
          columns={columns}
          dataSource={sales}
          loading={salesPending}
          pagination={false}
          locale={{ emptyText: t("customers.history.empty") }}
        />
        {hasNextPage && (
          <div style={{ textAlign: "center", marginTop: 16 }}>
            <Button loading={isFetchingNextPage} onClick={() => fetchNextPage()}>
              {t("common.loadMore")}
            </Button>
          </div>
        )}
      </Card>
    </Space>
  );
}
