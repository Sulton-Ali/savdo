import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { App, Button, Card, Popconfirm, Skeleton, Space, Typography } from "antd";
import { useTranslation } from "react-i18next";

import { createTelegramLink, deleteTelegramLink, fetchTelegramLinkStatus } from "../../auth/api";

const TELEGRAM_LINK_QUERY_KEY = ["auth", "telegramLink"] as const;

/**
 * Settings → Telegram (Phase 7 T7, deliverable C): `GET
 * /auth/telegram/link` status, `POST` to start linking (single-use 10-min
 * code + deep link), `DELETE` to unlink. Any authenticated role — see
 * `telegramLinkRoute`'s doc comment for why this isn't gated by
 * `shop.settings` like the rest of `/settings/*`.
 */
export function TelegramLinkPage() {
  const { t } = useTranslation();
  const { notification } = App.useApp();
  const queryClient = useQueryClient();

  const statusQuery = useQuery({
    queryKey: TELEGRAM_LINK_QUERY_KEY,
    queryFn: fetchTelegramLinkStatus,
  });

  const linkMutation = useMutation({
    mutationFn: createTelegramLink,
    onError: () => notification.error({ message: t("errors.generic") }),
  });

  const unlinkMutation = useMutation({
    mutationFn: deleteTelegramLink,
    onSuccess: async () => {
      linkMutation.reset();
      await queryClient.invalidateQueries({ queryKey: TELEGRAM_LINK_QUERY_KEY });
      notification.success({ message: t("telegram.unlinked") });
    },
    onError: () => notification.error({ message: t("errors.generic") }),
  });

  async function refresh() {
    // A fresh status check should stop showing a code that may already
    // have been used or expired — `getTelegramLink` is the source of truth.
    linkMutation.reset();
    await queryClient.invalidateQueries({ queryKey: TELEGRAM_LINK_QUERY_KEY });
  }

  if (statusQuery.isPending) {
    return (
      <Card title={t("telegram.title")} style={{ maxWidth: 480 }}>
        <Skeleton active />
      </Card>
    );
  }

  if (statusQuery.isError || !statusQuery.data) {
    return (
      <Card title={t("telegram.title")} style={{ maxWidth: 480 }}>
        <Typography.Paragraph type="danger">{t("errors.generic")}</Typography.Paragraph>
        <Button onClick={refresh}>{t("telegram.refresh")}</Button>
      </Card>
    );
  }

  const status = statusQuery.data;

  return (
    <Card title={t("telegram.title")} style={{ maxWidth: 480 }}>
      {status.linked ? (
        <>
          <Typography.Paragraph>
            {status.telegramUsername
              ? t("telegram.status.linked", { username: status.telegramUsername })
              : t("telegram.status.linkedNoUsername")}
          </Typography.Paragraph>
          <Space>
            <Button onClick={refresh} loading={statusQuery.isFetching}>
              {t("telegram.refresh")}
            </Button>
            <Popconfirm
              title={t("telegram.unlinkConfirm")}
              onConfirm={() => unlinkMutation.mutate()}
            >
              <Button danger loading={unlinkMutation.isPending}>
                {t("telegram.unlink")}
              </Button>
            </Popconfirm>
          </Space>
        </>
      ) : (
        <>
          <Typography.Paragraph>{t("telegram.status.notLinked")}</Typography.Paragraph>
          {linkMutation.data ? (
            <>
              <Typography.Paragraph>
                <Typography.Text strong copyable>
                  {linkMutation.data.code}
                </Typography.Text>
              </Typography.Paragraph>
              <Typography.Paragraph>
                <a href={linkMutation.data.deepLink} target="_blank" rel="noreferrer">
                  {t("telegram.openInTelegram")}
                </a>
              </Typography.Paragraph>
              <Typography.Text type="secondary">{t("telegram.codeValidity")}</Typography.Text>
              <div style={{ marginTop: 16 }}>
                <Button onClick={refresh} loading={statusQuery.isFetching}>
                  {t("telegram.refresh")}
                </Button>
              </div>
            </>
          ) : (
            <Button
              type="primary"
              onClick={() => linkMutation.mutate()}
              loading={linkMutation.isPending}
            >
              {t("telegram.link")}
            </Button>
          )}
        </>
      )}
    </Card>
  );
}
