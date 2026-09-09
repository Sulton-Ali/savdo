import * as Linking from "expo-linking";
import { useTranslation } from "react-i18next";
import { Alert, View } from "react-native";

import {
  useCreateTelegramLink,
  useDeleteTelegramLink,
  useTelegramLinkStatus,
} from "@/features/account/hooks";
import { deriveTelegramLinkViewState } from "@/features/account/telegramLink";

import { Button } from "./ui/button";
import { Text } from "./ui/text";

/**
 * The Settings sheet's Telegram section (Phase 7 T7 deliverable D): link
 * status, "Link Telegram" (shows the single-use code and opens the bot deep
 * link via `expo-linking`, already a dependency — D-78), a status refresh
 * and "Unlink". Any authenticated role links their own account (the
 * contract's `/auth/telegram/link` operations need nothing beyond a valid
 * session, mirroring `admin/src/routes/app/TelegramLinkPage.tsx`). All
 * rendering branches from the pure `deriveTelegramLinkViewState`
 * (`features/account/telegramLink.ts`), unit-tested there — this component
 * itself stays untested per D-73/D-85.
 */
export function TelegramLinkSection() {
  const { t } = useTranslation();
  const statusQuery = useTelegramLinkStatus();
  const createLink = useCreateTelegramLink();
  const deleteLink = useDeleteTelegramLink();

  const state = deriveTelegramLinkViewState({
    statusPending: statusQuery.isPending,
    statusError: statusQuery.isError,
    status: statusQuery.data,
    createdLinkCode: createLink.data,
  });

  function refresh() {
    // A re-checked status makes any earlier code stale (already used, or
    // its 10 minutes ran out) — drop it so a re-render can't show a dead
    // code next to a status that has since moved on.
    createLink.reset();
    void statusQuery.refetch();
  }

  function handleUnlink() {
    Alert.alert(t("telegram.unlinkConfirm"), undefined, [
      { text: t("common.cancel"), style: "cancel" },
      {
        text: t("telegram.unlink"),
        style: "destructive",
        onPress: () => deleteLink.mutate(undefined, { onSuccess: () => createLink.reset() }),
      },
    ]);
  }

  return (
    <View className="gap-2">
      <Text variant="small">{t("telegram.title")}</Text>

      {state.kind === "loading" && <Text variant="muted">{t("common.loading")}</Text>}

      {state.kind === "error" && (
        <View className="gap-2">
          <Text variant="muted">{t("errors.generic")}</Text>
          <Button variant="outline" onPress={refresh}>
            <Text>{t("telegram.refresh")}</Text>
          </Button>
        </View>
      )}

      {state.kind === "unlinked" && (
        <Button
          variant="outline"
          onPress={() => createLink.mutate()}
          disabled={createLink.isPending}
        >
          <Text>{t("telegram.link")}</Text>
        </Button>
      )}

      {state.kind === "linkCreated" && (
        <View className="gap-2">
          <Text variant="large">{state.code}</Text>
          <Button variant="outline" onPress={() => void Linking.openURL(state.deepLink)}>
            <Text>{t("telegram.openInTelegram")}</Text>
          </Button>
          <Text variant="muted">{t("telegram.codeValidity")}</Text>
          <Button variant="outline" onPress={refresh}>
            <Text>{t("telegram.refresh")}</Text>
          </Button>
        </View>
      )}

      {state.kind === "linked" && (
        <View className="gap-2">
          <Text>
            {state.username
              ? t("telegram.status.linked", { username: state.username })
              : t("telegram.status.linkedNoUsername")}
          </Text>
          <Button variant="destructive" onPress={handleUnlink} disabled={deleteLink.isPending}>
            <Text>{t("telegram.unlink")}</Text>
          </Button>
        </View>
      )}
    </View>
  );
}
