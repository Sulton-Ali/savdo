import { useQueries, useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Button, Card, Checkbox, Space, Table } from "antd";
import type { ColumnsType } from "antd/es/table";
import dayjs from "dayjs";
import type { TFunction } from "i18next";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";

import { useAuth } from "../../auth/AuthContext";
import { fetchCustomer } from "../../customers/api";
import { formatMoneyDisplay } from "../../lib/money";
import { useCursorList } from "../../lib/useCursorList";
import { fetchSaleDraftsPage, type SaleDraft } from "../../sales/draftsApi";
import { type DraftAgeUnit, draftAge } from "../../sales/draftsHelpers";
import { fetchStaffPage } from "../../staff/api";

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
 * `createdBy` on the wire is only a staff id — unlike `Sale.cashierName` or
 * `StockMovement.createdByName`, `SaleDraft` has no server-resolved display
 * name (open contract gap, flagged in this task's report). This page shows
 * "You" for the caller's own drafts, resolves other creators' names via
 * `GET /staff` for the owner (the one role that endpoint allows), and falls
 * back to a dash for a manager or cashier looking at someone else's draft.
 */
export function SaleDraftsListPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { me, isOwner } = useAuth();

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

  // Owner-only: the one role `GET /staff` allows (`docs/05-API.md`), used
  // here purely to resolve another staff member's `createdBy` id to a
  // display name — see the page doc comment above.
  const { data: staffPage } = useQuery({
    queryKey: ["staff-options"],
    queryFn: () => fetchStaffPage(null),
    enabled: isOwner,
  });
  const staffNameById = useMemo(() => {
    const map = new Map<string, string>();
    for (const staffMember of staffPage?.items ?? []) {
      map.set(staffMember.id, staffMember.fullName);
    }
    return map;
  }, [staffPage]);

  // Same N+1-by-id pattern `StockLevelsPage` uses for its product lookups —
  // a small, client-cached page of drafts, one `GET /customers/{id}` per
  // distinct customer (`cashier+`, so every role here can resolve it).
  const customerIds = useMemo(
    () =>
      Array.from(
        new Set(drafts.map((draft) => draft.customerId).filter((id): id is string => id != null)),
      ),
    [drafts],
  );
  const customerQueries = useQueries({
    queries: customerIds.map((id) => ({
      queryKey: ["customer", id],
      queryFn: () => fetchCustomer(id),
    })),
  });
  const customerNameById = useMemo(() => {
    const map = new Map<string, string>();
    customerIds.forEach((id, index) => {
      const name = customerQueries[index]?.data?.fullName;
      if (name) {
        map.set(id, name);
      }
    });
    return map;
  }, [customerIds, customerQueries]);

  function renderCreatedBy(draft: SaleDraft): string {
    if (draft.createdBy === me.user.id) {
      return t("sales.drafts.createdByMe");
    }
    if (draft.createdBy && staffNameById.has(draft.createdBy)) {
      return staffNameById.get(draft.createdBy) as string;
    }
    return "—";
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
      render: (_, draft) =>
        draft.customerId ? (customerNameById.get(draft.customerId) ?? "…") : "—",
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
