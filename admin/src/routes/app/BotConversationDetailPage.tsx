import { useNavigate } from "@tanstack/react-router";
import { Button, Card, Collapse, Skeleton, Space, Typography } from "antd";
import dayjs from "dayjs";
import { ArrowLeft } from "lucide-react";
import { useTranslation } from "react-i18next";

import { type BotMessage, useBotConversationMessages } from "../../lib/bot";

const ROLE_ALIGN: Record<BotMessage["role"], "flex-start" | "flex-end" | "center"> = {
  user: "flex-start",
  assistant: "flex-end",
  tool: "center",
};

const ROLE_BACKGROUND: Record<BotMessage["role"], string> = {
  user: "#f5f5f5",
  assistant: "#e6f4ff",
  tool: "#fff7e6",
};

/** The meta line under an `assistant` bubble — provider, model, token
 * counts, latency and cost estimate (`docs/05-API.md` § Bot, O-28). `null`
 * on every `user`/`tool` row (only an assistant reply calls the LLM), so
 * this renders nothing for those. */
function MessageMeta({ message }: { message: BotMessage }) {
  const { t } = useTranslation();

  if (message.provider === null) {
    return null;
  }

  const parts = [
    [message.provider, message.model].filter(Boolean).join(" · "),
    message.inputTokens !== null && message.outputTokens !== null
      ? t("bot.detail.tokens", { input: message.inputTokens, output: message.outputTokens })
      : null,
    message.latencyMs !== null ? t("bot.detail.latency", { ms: message.latencyMs }) : null,
    message.costEstimate !== null ? t("bot.detail.cost", { cost: message.costEstimate }) : null,
  ].filter((part): part is string => Boolean(part));

  return (
    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
      {parts.join(" · ")}
    </Typography.Text>
  );
}

function MessageBubble({ message }: { message: BotMessage }) {
  const { t } = useTranslation();

  return (
    <div style={{ display: "flex", justifyContent: ROLE_ALIGN[message.role] }}>
      <Card
        size="small"
        style={{ maxWidth: "70%", background: ROLE_BACKGROUND[message.role] }}
        styles={{ body: { padding: 12 } }}
      >
        <Space direction="vertical" size={4} style={{ width: "100%" }}>
          <Typography.Text strong style={{ fontSize: 12 }}>
            {t(`bot.detail.role.${message.role}`)}
          </Typography.Text>
          <Typography.Text style={{ whiteSpace: "pre-wrap" }}>{message.content}</Typography.Text>
          <MessageMeta message={message} />
          {message.toolCalls !== null && (
            <Collapse
              size="small"
              items={[
                {
                  key: "tool-calls",
                  label: t("bot.detail.toolCalls"),
                  children: (
                    <pre style={{ margin: 0, fontSize: 12, whiteSpace: "pre-wrap" }}>
                      {JSON.stringify(message.toolCalls, null, 2)}
                    </pre>
                  ),
                },
              ]}
            />
          )}
          <Typography.Text type="secondary" style={{ fontSize: 11 }}>
            {dayjs(message.createdAt).format("YYYY-MM-DD HH:mm:ss")}
          </Typography.Text>
        </Space>
      </Card>
    </div>
  );
}

/**
 * One conversation's transcript, oldest first (`GET
 * /bot/conversations/{id}/messages`, `bot.read`, owner/manager). The
 * endpoint returns only messages, not the conversation's chat id/Telegram
 * username (that lives on `BotConversation`, one level up, with no
 * single-conversation `GET`) — the header stays generic rather than
 * guessing at identifying info the API does not hand this page.
 */
export function BotConversationDetailPage({ conversationId }: { conversationId: string }) {
  const { t } = useTranslation();
  const navigate = useNavigate();

  const { data, fetchNextPage, hasNextPage, isFetchingNextPage, isPending, isError } =
    useBotConversationMessages(conversationId);
  const messages = data?.pages.flatMap((page) => page.items) ?? [];

  const header = (
    <Space>
      <Button
        type="text"
        aria-label={t("common.back")}
        icon={<ArrowLeft size={16} />}
        onClick={() => navigate({ to: "/bot/conversations" })}
      />
      <Typography.Text strong>{t("bot.detail.title")}</Typography.Text>
    </Space>
  );

  if (isPending) {
    return (
      <Card title={header}>
        <Skeleton active />
      </Card>
    );
  }

  if (isError) {
    return (
      <Card title={header}>
        <Typography.Text>{t("bot.detail.notFound")}</Typography.Text>
      </Card>
    );
  }

  return (
    <Card title={header}>
      {messages.length === 0 ? (
        <Typography.Text type="secondary">{t("bot.detail.empty")}</Typography.Text>
      ) : (
        <Space direction="vertical" size="middle" style={{ width: "100%" }}>
          {messages.map((message) => (
            <MessageBubble key={message.id} message={message} />
          ))}
        </Space>
      )}
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
