import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import {
  App,
  Button,
  Card,
  Descriptions,
  Popconfirm,
  Skeleton,
  Space,
  Table,
  Tag,
  Typography,
} from "antd";
import type { ColumnsType } from "antd/es/table";
import { ArrowLeft } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";

import { useAuth } from "../../auth/AuthContext";
import { fetchCustomer } from "../../customers/api";
import { ApiError, notifyApiError } from "../../lib/errors";
import { formatMoneyDisplay } from "../../lib/money";
import { fetchLocationsPage } from "../../locations/api";
import { deleteSaleDraft, fetchSaleDraft, type SaleDraftItem } from "../../sales/draftsApi";
import { canManageDraft } from "../../sales/draftsHelpers";
import { DraftEditModal } from "./sale-draft-detail/DraftEditModal";
import { DraftPayModal } from "./sale-draft-detail/DraftPayModal";

/**
 * A draft sale's full record — a shared, unpaid order any `cashier+` staff
 * may open (D-87). Pay completes it under the viewing staff member,
 * regardless of who created it (D-96); Edit/Delete stay restricted to the
 * draft's own creator or manager+ (D-89, `canManageDraft`). Line items are
 * read-only here — editing them is a separate, not-yet-built task. A
 * `NOT_FOUND` from any action (the draft was completed or deleted by
 * someone else in the meantime) sends the user back to the drafts list.
 */
export function SaleDraftDetailPage({ draftId }: { draftId: string }) {
  const { t } = useTranslation();
  const { notification } = App.useApp();
  const { me, can } = useAuth();
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  const {
    data: draft,
    isPending,
    isError,
    error,
  } = useQuery({
    queryKey: ["sale-draft", draftId],
    queryFn: () => fetchSaleDraft(draftId),
  });

  // biome-ignore lint/correctness/useExhaustiveDependencies: `notification`/`navigate`/`t` are stable across renders (App.useApp()/useNavigate()/i18next), only `isError`/`error` should retrigger this.
  useEffect(() => {
    if (isError && error instanceof ApiError && error.code === "NOT_FOUND") {
      notification.info({ message: t("sales.drafts.errors.notFound") });
      void navigate({ to: "/sales/drafts" });
    }
  }, [isError, error]);

  const { data: locationsPage } = useQuery({
    queryKey: ["locations-options"],
    queryFn: () => fetchLocationsPage(null),
  });
  const locationNameById = useMemo(() => {
    const map = new Map<string, string>();
    for (const location of locationsPage?.items ?? []) {
      map.set(location.id, location.name);
    }
    return map;
  }, [locationsPage]);

  const { data: customer } = useQuery({
    queryKey: ["customer", draft?.customerId],
    queryFn: () => fetchCustomer(draft?.customerId as string),
    enabled: draft?.customerId != null,
  });

  const [payOpen, setPayOpen] = useState(false);
  const [editOpen, setEditOpen] = useState(false);

  const deleteMutation = useMutation({
    mutationFn: () => deleteSaleDraft(draftId),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["sale-drafts"] });
      notification.success({ message: t("sales.drafts.delete.success") });
      await navigate({ to: "/sales/drafts" });
    },
    onError: (mutationError) => {
      if (mutationError instanceof ApiError && mutationError.code === "NOT_FOUND") {
        notification.info({ message: t("sales.drafts.errors.notFound") });
        void navigate({ to: "/sales/drafts" });
        return;
      }
      notifyApiError(notification, mutationError, t);
    },
  });

  const header = (
    <Space>
      <Button
        type="text"
        aria-label={t("common.back")}
        icon={<ArrowLeft size={16} />}
        onClick={() => navigate({ to: "/sales/drafts" })}
      />
      <Typography.Text strong>{t("sales.drafts.detail.title")}</Typography.Text>
    </Space>
  );

  if (isPending) {
    return (
      <Card title={header}>
        <Skeleton active />
      </Card>
    );
  }

  if (isError || !draft) {
    return (
      <Card title={header}>
        <Typography.Text>{t("sales.drafts.detail.notFound")}</Typography.Text>
      </Card>
    );
  }

  const canManage = canManageDraft(draft.createdBy, me.user.id, can("sales.void"));

  const itemColumns: ColumnsType<SaleDraftItem> = [
    {
      title: t("sales.drafts.detail.columns.product"),
      key: "product",
      render: (_, item) => (
        <Space>
          <span>{item.productName}</span>
          {!item.available && <Tag color="red">{t("sales.drafts.detail.unavailable")}</Tag>}
        </Space>
      ),
    },
    { title: t("sales.drafts.detail.columns.variant"), dataIndex: "variantLabel" },
    { title: t("sales.drafts.detail.columns.qty"), dataIndex: "qty" },
    {
      title: t("sales.drafts.detail.columns.unitPrice"),
      dataIndex: "unitPrice",
      render: (value: string) => formatMoneyDisplay(value),
    },
    {
      title: t("sales.drafts.detail.columns.lineTotal"),
      dataIndex: "lineTotal",
      render: (value: string) => formatMoneyDisplay(value),
    },
  ];

  const discountText = draft.discount
    ? `${t(`sales.discountType.${draft.discount.type}`)} ${draft.discount.value}${
        draft.discount.type === "percent" ? "%" : ""
      }`
    : "—";

  return (
    <Space direction="vertical" size="large" style={{ width: "100%" }}>
      <Card
        title={header}
        extra={
          <Space>
            <Button type="primary" onClick={() => setPayOpen(true)}>
              {t("sales.drafts.pay.action")}
            </Button>
            {canManage && (
              <Button onClick={() => setEditOpen(true)}>{t("sales.drafts.edit.action")}</Button>
            )}
            {canManage && (
              <Popconfirm
                title={t("sales.drafts.delete.confirm")}
                onConfirm={() => deleteMutation.mutate()}
              >
                <Button danger loading={deleteMutation.isPending}>
                  {t("sales.drafts.delete.action")}
                </Button>
              </Popconfirm>
            )}
          </Space>
        }
      >
        <Descriptions column={2} bordered size="small">
          <Descriptions.Item label={t("sales.drafts.detail.fields.location")}>
            {locationNameById.get(draft.locationId) ?? "—"}
          </Descriptions.Item>
          <Descriptions.Item label={t("sales.drafts.detail.fields.customer")}>
            {draft.customerId ? (customer?.fullName ?? "…") : "—"}
          </Descriptions.Item>
          <Descriptions.Item label={t("sales.drafts.detail.fields.note")}>
            {draft.note ?? "—"}
          </Descriptions.Item>
          <Descriptions.Item label={t("sales.drafts.detail.fields.discount")}>
            {discountText}
          </Descriptions.Item>
        </Descriptions>
      </Card>

      <Card title={t("sales.drafts.detail.items")}>
        <Table<SaleDraftItem>
          rowKey="variantId"
          columns={itemColumns}
          dataSource={draft.items}
          pagination={false}
        />
      </Card>

      <Card>
        <Descriptions column={1} bordered size="small">
          <Descriptions.Item label={t("sales.drafts.detail.totals.subtotal")}>
            {formatMoneyDisplay(draft.subtotal)}
          </Descriptions.Item>
          <Descriptions.Item label={t("sales.drafts.detail.totals.discount")}>
            {formatMoneyDisplay(draft.discountAmount)}
          </Descriptions.Item>
          <Descriptions.Item label={t("sales.drafts.detail.totals.total")}>
            {formatMoneyDisplay(draft.estimatedTotal)}
          </Descriptions.Item>
        </Descriptions>
      </Card>

      <DraftPayModal draft={draft} open={payOpen} onClose={() => setPayOpen(false)} />
      <DraftEditModal
        draft={draft}
        open={editOpen}
        onClose={() => setEditOpen(false)}
        onUpdated={(updated) => queryClient.setQueryData(["sale-draft", draftId], updated)}
      />
    </Space>
  );
}
