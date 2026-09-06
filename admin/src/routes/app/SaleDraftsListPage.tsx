import { useNavigate } from "@tanstack/react-router";
import { Button, Card, Checkbox, Space, Table } from "antd";
import type { ColumnsType } from "antd/es/table";
import dayjs from "dayjs";
import type { TFunction } from "i18next";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";

import { useAuth } from "../../auth/AuthContext";
import { formatMoneyDisplay } from "../../lib/money";
import { useCursorList } from "../../lib/useCursorList";
import { fetchSaleDraftsPage, type SaleDraft } from "../../sales/draftsApi";
import { type DraftAgeUnit, draftAge } from "../../sales/draftsHelpers";

/** Formats a `SaleDraft.createdAt` age bucket as the drafts list's
 * secondary "5 minutes ago" text next to the formatted timestamp — the
 * `_one`/`_few`/`_many`/`_other` i18next plural keys under `sales.drafts.age`
 * (`packages/i18n`) cover every locale's plural rules for the same three
 * buckets `draftAge` computes. */
function formatDraftAge(unit: DraftAgeUnit, value: number, t: TFunction): string {
  if (unit === "justNow") {
    return t("sales.drafts.age.justNow");
  }
  return t(`sales.drafts.age.${unit}`, { count: value });
}

/**
 * List every draft sale in the shop (`docs/00-DECISIONS.md` D-87: shared
 * across staff and devices, `cashier+` may list every draft, not only their
 * own). Newest first, cursor-paginated (`docs/05-API.md`). Row click opens
 * `SaleDraftDetailPage`. The "my drafts only" checkbox narrows the list via
 * `createdBy=<me>`, the same query param `GET /sales/drafts` documents.
 *
 * `createdByName`, resolved server-side (mirrors `StockMovement.createdByName`),
 * is shown for another staff member's draft; the caller's own draft shows
 * "You" instead of their own name, and a `null` name (creator deleted or
 * never recorded) shows a dash. `customerName` is resolved server-side the
 * same way (T18) — no per-row `GET /customers/{id}` lookup here.
 */
export function SaleDraftsListPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { me } = useAuth();

  const [mineOnly, setMineOnly] = useState(false);
  const filters = useMemo(
    () => ({ createdBy: mineOnly ? me.user.id : undefined }),
    [mineOnly, me.user.id],
  );

  const { data, fetchNextPage, hasNextPage, isFetchingNextPage, isPending } = useCursorList(
    ["sale-drafts", filters],
    (cursor) => fetchSaleDraftsPage(filters, cursor),
  );
  const drafts = data?.pages.flatMap((page) => page.items) ?? [];

  function renderCreatedBy(draft: SaleDraft): string {
    if (draft.createdBy === me.user.id) {
      return t("sales.drafts.createdByMe");
    }
    return draft.createdByName ?? "—";
  }

  const columns: ColumnsType<SaleDraft> = [
    {
      title: t("sales.drafts.columns.createdAt"),
      key: "createdAt",
      render: (_, draft) => {
        const age = draftAge(draft.createdAt);
        return (
          <Space direction="vertical" size={0}>
            <span>{dayjs(draft.createdAt).format("YYYY-MM-DD HH:mm")}</span>
            <span style={{ color: "rgba(0,0,0,0.45)", fontSize: 12 }}>
              {formatDraftAge(age.unit, age.value, t)}
            </span>
          </Space>
        );
      },
    },
    {
      title: t("sales.drafts.columns.createdBy"),
      key: "createdBy",
      render: (_, draft) => renderCreatedBy(draft),
    },
    {
      title: t("sales.drafts.columns.customer"),
      key: "customer",
      render: (_, draft) => draft.customerName ?? "—",
    },
    {
      title: t("sales.drafts.columns.lines"),
      key: "lines",
      render: (_, draft) => draft.items.length,
    },
    {
      title: t("sales.drafts.columns.estimatedTotal"),
      dataIndex: "estimatedTotal",
      render: (value: string) => formatMoneyDisplay(value),
    },
    {
      title: t("sales.drafts.columns.note"),
      dataIndex: "note",
      render: (value: string | null) => value ?? "—",
    },
  ];

  return (
    <Card title={t("sales.drafts.listTitle")}>
      <Space style={{ marginBottom: 16 }}>
        <Checkbox checked={mineOnly} onChange={(event) => setMineOnly(event.target.checked)}>
          {t("sales.drafts.filters.mine")}
        </Checkbox>
      </Space>

      <Table<SaleDraft>
        rowKey="id"
        columns={columns}
        dataSource={drafts}
        loading={isPending}
        pagination={false}
        locale={{ emptyText: t("sales.drafts.empty") }}
        onRow={(row) => ({
          onClick: () => navigate({ to: "/sales/drafts/$id", params: { id: row.id } }),
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
