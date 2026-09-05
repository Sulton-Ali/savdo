import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Button, Card, Descriptions, Skeleton, Space, Table, Tag, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import dayjs from "dayjs";
import { ArrowLeft } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { useAuth } from "../../auth/AuthContext";
import { formatMoney, parseMoney } from "../../lib/money";
import { fetchSale, type Sale, type SaleItem } from "../../sales/api";
import { SaleReturnDrawer } from "./sale-detail/SaleReturnDrawer";
import { SaleVoidModal } from "./sale-detail/SaleVoidModal";

const SALE_STATUS_COLORS: Record<Sale["status"], string> = {
  completed: "green",
  voided: "red",
};

/**
 * A sale's full record — number, parties, payment, line items and totals
 * (D-53: there is no separate receipt, this page is the record). Void and
 * return are manager+ (`sales.void`, docs/04-DATA-MODEL.md § 7); the button
 * that opens each stays hidden for anyone without it, and both stay hidden
 * on anything but a completed `kind: sale` (a `kind: return` is itself
 * final and a voided sale cannot be voided or returned again, D-59, D-66).
 * Void additionally hides once a return already references this sale
 * (D-62); return does not, since further partial returns are allowed. The
 * server re-checks all of this regardless. `unitCost` on the items table
 * only renders when at least one item on this sale actually carries it
 * (`cost.read`, ADR-010) — a cashier's response never has it, so the column
 * itself must not assume it exists.
 */
export function SaleDetailPage({ saleId }: { saleId: string }) {
  const { t } = useTranslation();
  const { can } = useAuth();
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  const {
    data: sale,
    isPending,
    isError,
  } = useQuery({
    queryKey: ["sale", saleId],
    queryFn: () => fetchSale(saleId),
  });

  const [voidOpen, setVoidOpen] = useState(false);
  const [returnOpen, setReturnOpen] = useState(false);

  const canVoid = can("sales.void");

  const header = (
    <Space>
      <Button
        type="text"
        aria-label={t("common.back")}
        icon={<ArrowLeft size={16} />}
        onClick={() => navigate({ to: "/sales" })}
      />
      <Typography.Text strong>{t("sales.detail.title")}</Typography.Text>
    </Space>
  );

  if (isPending) {
    return (
      <Card title={header}>
        <Skeleton active />
      </Card>
    );
  }

  if (isError || !sale) {
    return (
      <Card title={header}>
        <Typography.Text>{t("sales.detail.notFound")}</Typography.Text>
      </Card>
    );
  }

  const canShowVoid =
    canVoid && sale.kind === "sale" && sale.status === "completed" && !sale.hasReturns;
  // A return is itself final (D-66): only a completed `kind: sale` sale can
  // be returned, voided or not-yet-voided the same way void requires — but
  // unlike void, `hasReturns` does not hide this button, since further
  // partial returns against the same sale are allowed.
  const canShowReturn = canVoid && sale.kind === "sale" && sale.status === "completed";
  const hasCostColumn = sale.items.some((item) => item.unitCost !== undefined);

  const itemColumns: ColumnsType<SaleItem> = [
    { title: t("sales.detail.columns.product"), dataIndex: "productName" },
    { title: t("sales.detail.columns.variant"), dataIndex: "variantLabel" },
    { title: t("sales.detail.columns.qty"), dataIndex: "qty" },
    {
      title: t("sales.detail.columns.unitPrice"),
      dataIndex: "unitPrice",
      render: (value: string) => formatMoney(parseMoney(value)) ?? "—",
    },
    {
      title: t("sales.detail.columns.lineTotal"),
      dataIndex: "lineTotal",
      render: (value: string) => formatMoney(parseMoney(value)) ?? "—",
    },
    { title: t("sales.detail.columns.returnedQty"), dataIndex: "returnedQty" },
    ...(hasCostColumn
      ? ([
          {
            title: t("sales.detail.columns.unitCost"),
            dataIndex: "unitCost",
            render: (value: string | undefined) =>
              value === undefined ? "—" : (formatMoney(parseMoney(value)) ?? "—"),
          },
        ] satisfies ColumnsType<SaleItem>)
      : []),
  ];

  return (
    <Space direction="vertical" size="large" style={{ width: "100%" }}>
      <Card
        title={header}
        extra={
          <Space>
            {canShowReturn && (
              <Button onClick={() => setReturnOpen(true)}>{t("sales.return.action")}</Button>
            )}
            {canShowVoid && (
              <Button danger onClick={() => setVoidOpen(true)}>
                {t("sales.void.action")}
              </Button>
            )}
          </Space>
        }
      >
        <Descriptions column={2} bordered size="small">
          <Descriptions.Item label={t("sales.detail.fields.number")}>
            {sale.number}
          </Descriptions.Item>
          <Descriptions.Item label={t("sales.detail.fields.kind")}>
            {t(`sales.kind.${sale.kind}`)}
          </Descriptions.Item>
          <Descriptions.Item label={t("sales.detail.fields.status")}>
            <Tag color={SALE_STATUS_COLORS[sale.status]}>{t(`sales.status.${sale.status}`)}</Tag>
          </Descriptions.Item>
          <Descriptions.Item label={t("sales.detail.fields.date")}>
            {dayjs(sale.completedAt).format("YYYY-MM-DD HH:mm")}
          </Descriptions.Item>
          <Descriptions.Item label={t("sales.detail.fields.location")}>
            {sale.locationName}
          </Descriptions.Item>
          <Descriptions.Item label={t("sales.detail.fields.cashier")}>
            {sale.cashierName}
          </Descriptions.Item>
          <Descriptions.Item label={t("sales.detail.fields.customer")}>
            {sale.customerName ?? "—"}
          </Descriptions.Item>
          <Descriptions.Item label={t("sales.detail.fields.payment")}>
            {t(`sales.paymentMethod.${sale.payment.method}`)} —{" "}
            {formatMoney(parseMoney(sale.payment.amount)) ?? "—"}
          </Descriptions.Item>
          <Descriptions.Item label={t("sales.detail.fields.note")}>
            {sale.note ?? "—"}
          </Descriptions.Item>
          <Descriptions.Item label={t("sales.detail.fields.hasReturns")}>
            {sale.hasReturns ? t("sales.detail.yes") : t("sales.detail.no")}
          </Descriptions.Item>
          {sale.kind === "return" && sale.originalSaleId && (
            <Descriptions.Item label={t("sales.detail.fields.originalSale")}>
              <Button
                type="link"
                style={{ padding: 0 }}
                onClick={() =>
                  navigate({ to: "/sales/$id", params: { id: sale.originalSaleId as string } })
                }
              >
                {t("sales.detail.viewOriginal")}
              </Button>
            </Descriptions.Item>
          )}
          {sale.status === "voided" && (
            <>
              <Descriptions.Item label={t("sales.detail.fields.voidedAt")}>
                {sale.voidedAt ? dayjs(sale.voidedAt).format("YYYY-MM-DD HH:mm") : "—"}
              </Descriptions.Item>
              <Descriptions.Item label={t("sales.detail.fields.voidedBy")}>
                {sale.voidedBy ?? "—"}
              </Descriptions.Item>
              <Descriptions.Item label={t("sales.detail.fields.voidReason")}>
                {sale.voidReason ?? "—"}
              </Descriptions.Item>
            </>
          )}
        </Descriptions>
      </Card>

      <Card title={t("sales.detail.items")}>
        <Table<SaleItem>
          rowKey="id"
          columns={itemColumns}
          dataSource={sale.items}
          pagination={false}
        />
      </Card>

      <Card>
        <Descriptions column={1} bordered size="small">
          <Descriptions.Item label={t("sales.detail.totals.subtotal")}>
            {formatMoney(parseMoney(sale.subtotal)) ?? "—"}
          </Descriptions.Item>
          <Descriptions.Item label={t("sales.detail.totals.discount")}>
            {formatMoney(parseMoney(sale.discountAmount)) ?? "—"}
            {sale.discountReason ? ` — ${sale.discountReason}` : ""}
          </Descriptions.Item>
          <Descriptions.Item label={t("sales.detail.totals.total")}>
            {formatMoney(parseMoney(sale.total)) ?? "—"}
          </Descriptions.Item>
        </Descriptions>
      </Card>

      <SaleVoidModal
        sale={sale}
        open={voidOpen}
        onClose={() => setVoidOpen(false)}
        onVoided={(voided) => queryClient.setQueryData(["sale", saleId], voided)}
      />
      <SaleReturnDrawer sale={sale} open={returnOpen} onClose={() => setReturnOpen(false)} />
    </Space>
  );
}
