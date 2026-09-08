import { useNavigate } from "@tanstack/react-router";
import { Button, Card, Table } from "antd";
import type { ColumnsType } from "antd/es/table";
import dayjs from "dayjs";
import { useTranslation } from "react-i18next";

import { type BotConversation, useBotConversations } from "../../lib/bot";

/**
 * Every Telegram chat that has talked to the bot (`docs/05-API.md` § Bot,
 * `bot.read`, owner/manager only — gated by the route and nav entry, not
 * this page). Newest activity first, per `GET /bot/conversations`. No
 * per-conversation cost column: `BotConversation` carries no cost field (it
 * is on `BotMessage`, one level down), so a per-row total is not cheap to
 * derive from this page's response — it is shown per message instead, on
 * the detail page.
 */
export function BotConversationsListPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();

  const { data, fetchNextPage, hasNextPage, isFetchingNextPage, isPending } = useBotConversations();
  const conversations = data?.pages.flatMap((page) => page.items) ?? [];

  const columns: ColumnsType<BotConversation> = [
    {
      title: t("bot.columns.chat"),
      dataIndex: "telegramUsername",
      render: (_value: string | null, row: BotConversation) =>
        row.telegramUsername ? `@${row.telegramUsername}` : row.telegramChatId,
    },
    { title: t("bot.columns.messageCount"), dataIndex: "messageCount" },
    {
      title: t("bot.columns.lastMessageAt"),
      dataIndex: "lastMessageAt",
      render: (value: string | null) => (value ? dayjs(value).format("YYYY-MM-DD HH:mm") : "—"),
    },
  ];

  return (
    <Card title={t("bot.listTitle")}>
      <Table<BotConversation>
        rowKey="id"
        columns={columns}
        dataSource={conversations}
        loading={isPending}
        pagination={false}
        locale={{ emptyText: t("bot.empty") }}
        onRow={(row) => ({
          onClick: () => navigate({ to: "/bot/conversations/$id", params: { id: row.id } }),
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
