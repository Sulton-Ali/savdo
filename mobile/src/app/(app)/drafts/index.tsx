import { useRouter } from "expo-router";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { ActivityIndicator, FlatList, Pressable, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";

import { Text } from "@/components/ui/text";
import type { SaleDraft } from "@/features/sales/api";
import { draftAge } from "@/features/sales/drafts";
import { useDrafts } from "@/features/sales/hooks";
import { formatMoney } from "@/lib/money";
import { useSession } from "@/lib/session";

function DraftAgeText({ createdAt }: { createdAt: string }) {
  const { t } = useTranslation();
  const age = draftAge(createdAt);
  return <Text variant="muted">{t(`mobile.drafts.age.${age.unit}`, { count: age.value })}</Text>;
}

/**
 * `draft.createdByName` is resolved server-side (mirrors
 * `StockMovement.createdByName`, T17, merged into `main` after this task's
 * branch was cut — rebased onto it rather than shipping the
 * you/other-staff-only workaround an earlier version of this file used
 * while the contract still lacked it). Shown as "You" for the caller's own
 * draft rather than their own name, otherwise the resolved name, falling
 * back to "Unknown" for a draft whose creator has since been removed
 * (`createdByName: null`, `SaleDraft`'s own doc comment).
 */
function DraftCreatedByText({
  createdBy,
  createdByName,
}: {
  createdBy: string | null;
  createdByName: string | null;
}) {
  const { t } = useTranslation();
  const { me } = useSession();
  if (createdBy && createdBy === me?.user.id) {
    return <Text variant="muted">{t("mobile.drafts.createdBy.you")}</Text>;
  }
  return (
    <Text variant="muted" numberOfLines={1}>
      {createdByName ?? t("mobile.drafts.createdBy.unknown")}
    </Text>
  );
}

function DraftRow({
  draft,
  currency,
  onPress,
}: {
  draft: SaleDraft;
  currency: string;
  onPress: () => void;
}) {
  const { t } = useTranslation();
  return (
    <Pressable
      accessibilityRole="button"
      className="min-h-16 gap-1 rounded-md border border-border bg-card p-3 active:bg-accent"
      onPress={onPress}
    >
      <View className="flex-row items-center justify-between">
        <DraftAgeText createdAt={draft.createdAt} />
        <Text variant="large">{formatMoney(draft.estimatedTotal, currency)}</Text>
      </View>
      <View className="flex-row items-center justify-between">
        <Text variant="muted" numberOfLines={1}>
          {draft.customerName ?? "—"}
        </Text>
        <Text variant="muted">
          {t("mobile.drafts.list.lineCount", { count: draft.items.length })}
        </Text>
      </View>
      <DraftCreatedByText createdBy={draft.createdBy} createdByName={draft.createdByName} />
    </Pressable>
  );
}

/**
 * Drafts list (T14 deliverable 2, D-87..D-90): every draft in the shop,
 * newest first, cursor-paginated (`GET /sales/drafts`), with a "mine"
 * toggle (`createdBy` = the caller's own id) and pull to refresh. Any
 * `cashier+` may list and open any draft (D-87: shared across staff and
 * devices) — no permission gate here, unlike Edit/Delete on the detail
 * screen (D-89).
 */
export default function DraftsListScreen() {
  const { t } = useTranslation();
  const router = useRouter();
  const insets = useSafeAreaInsets();
  const { shop, me } = useSession();
  const currency = shop?.currency ?? "UZS";
  const [mineOnly, setMineOnly] = useState(false);

  const {
    data,
    isPending,
    isError,
    isRefetching,
    refetch,
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
  } = useDrafts({ createdBy: mineOnly ? me?.user.id : undefined });
  const drafts = useMemo(() => data?.pages.flatMap((page) => page.items) ?? [], [data]);

  return (
    <View className="flex-1 bg-background">
      <View className="flex-row gap-2 px-4 pt-4">
        <Pressable
          accessibilityRole="button"
          accessibilityState={{ selected: !mineOnly }}
          className={`h-10 flex-1 items-center justify-center rounded-md border ${
            !mineOnly ? "border-primary bg-primary" : "border-input bg-background"
          }`}
          onPress={() => setMineOnly(false)}
        >
          <Text className={!mineOnly ? "text-primary-foreground" : undefined}>
            {t("mobile.drafts.filter.all")}
          </Text>
        </Pressable>
        <Pressable
          accessibilityRole="button"
          accessibilityState={{ selected: mineOnly }}
          className={`h-10 flex-1 items-center justify-center rounded-md border ${
            mineOnly ? "border-primary bg-primary" : "border-input bg-background"
          }`}
          onPress={() => setMineOnly(true)}
        >
          <Text className={mineOnly ? "text-primary-foreground" : undefined}>
            {t("mobile.drafts.filter.mine")}
          </Text>
        </Pressable>
      </View>

      {isPending ? (
        <View className="flex-1 items-center justify-center">
          <ActivityIndicator />
        </View>
      ) : isError ? (
        <View className="flex-1 items-center justify-center gap-2 p-6">
          <Text variant="muted">{t("errors.generic")}</Text>
          <Pressable accessibilityRole="button" onPress={() => refetch()}>
            <Text className="text-primary">{t("common.retry")}</Text>
          </Pressable>
        </View>
      ) : (
        <FlatList
          className="flex-1 px-4"
          contentContainerStyle={{ paddingTop: 12, paddingBottom: insets.bottom + 12, gap: 8 }}
          data={drafts}
          keyExtractor={(draft) => draft.id}
          refreshing={isRefetching}
          onRefresh={refetch}
          onEndReachedThreshold={0.4}
          onEndReached={() => {
            if (hasNextPage && !isFetchingNextPage) {
              fetchNextPage();
            }
          }}
          ListEmptyComponent={
            <Text variant="muted" className="p-4 text-center">
              {t("mobile.drafts.list.empty")}
            </Text>
          }
          ListFooterComponent={isFetchingNextPage ? <ActivityIndicator className="py-4" /> : null}
          renderItem={({ item: draft }) => (
            <DraftRow
              draft={draft}
              currency={currency}
              onPress={() => router.push(`/drafts/${draft.id}`)}
            />
          )}
        />
      )}
    </View>
  );
}
