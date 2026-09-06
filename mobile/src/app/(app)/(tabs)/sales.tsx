import { useRouter } from "expo-router";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { ActivityIndicator, FlatList, Pressable, View } from "react-native";

import { Text } from "@/components/ui/text";
import type { SaleSummary } from "@/features/sales/api";
import { useSales } from "@/features/sales/hooks";
import { formatMoney } from "@/lib/money";
import { useSession } from "@/lib/session";

/** `date`'s calendar date (`YYYY-MM-DD`) in `timeZone` — mirrors
 * `features/catalog/pricing.ts`'s own (private) `calendarDateInTimeZone`. */
function calendarDateInTimeZone(date: Date, timeZone: string): string {
  return new Intl.DateTimeFormat("en-CA", {
    timeZone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).format(date);
}

function SaleRow({
  sale,
  timeZone,
  currency,
  onPress,
}: {
  sale: SaleSummary;
  timeZone: string;
  currency: string;
  onPress: () => void;
}) {
  const { t } = useTranslation();
  const time = new Intl.DateTimeFormat(undefined, {
    timeZone,
    hour: "2-digit",
    minute: "2-digit",
  }).format(new Date(sale.completedAt));

  return (
    <Pressable
      accessibilityRole="button"
      className="min-h-14 gap-1 rounded-md border border-border bg-card p-3 active:bg-accent"
      onPress={onPress}
    >
      <View className="flex-row items-center justify-between">
        <Text>
          {t("sales.columns.number")} {sale.number} · {time}
        </Text>
        <Text variant="large">{formatMoney(sale.total, currency)}</Text>
      </View>
      <View className="flex-row items-center justify-between">
        <Text variant="muted" numberOfLines={1}>
          {sale.customerName ?? t("mobile.sale.customer.none")}
        </Text>
        <Text variant="muted">
          {t(`sales.kind.${sale.kind}`)} · {t(`sales.paymentMethod.${sale.paymentMethod}`)}
          {sale.status === "voided" ? ` · ${t("sales.status.voided")}` : ""}
        </Text>
      </View>
    </Pressable>
  );
}

/**
 * Today's sales (T4 deliverable 3), cursor-paginated, newest first (`GET
 * /sales`). Deliberately scoped to today only, in the shop's own timezone
 * — a quick-sale companion list, not the full sales browser (`admin`'s
 * `SalesPage` already covers any day/location/cashier filter). D-63 lets
 * every `cashier+` role see every sale for the whole shop and any day
 * regardless — this screen's own "today" scope is a UI simplification,
 * not a permission restriction; flagged in this task's report since the
 * brief cited D-71 for it, which is actually about the *reports* summary
 * netting refunds to the original cashier, not this list's visibility.
 */
export default function SaleListScreen() {
  const { t } = useTranslation();
  const router = useRouter();
  const { shop } = useSession();
  const timeZone = shop?.timezone ?? Intl.DateTimeFormat().resolvedOptions().timeZone;
  const currency = shop?.currency ?? "UZS";
  const today = useMemo(() => calendarDateInTimeZone(new Date(), timeZone), [timeZone]);

  const {
    data,
    isPending,
    isError,
    isRefetching,
    refetch,
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
  } = useSales({ from: today, to: today });

  const sales = useMemo(() => data?.pages.flatMap((page) => page.items) ?? [], [data]);

  if (isPending) {
    return (
      <View className="flex-1 items-center justify-center bg-background">
        <ActivityIndicator />
      </View>
    );
  }

  if (isError) {
    return (
      <View className="flex-1 items-center justify-center gap-2 bg-background p-6">
        <Text variant="muted">{t("errors.generic")}</Text>
        <Pressable accessibilityRole="button" onPress={() => refetch()}>
          <Text className="text-primary">{t("common.retry")}</Text>
        </Pressable>
      </View>
    );
  }

  return (
    <View className="flex-1 bg-background">
      <FlatList
        className="flex-1 px-4"
        contentContainerStyle={{ paddingVertical: 12, gap: 8 }}
        data={sales}
        keyExtractor={(sale) => sale.id}
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
            {t("mobile.sale.list.empty")}
          </Text>
        }
        ListFooterComponent={isFetchingNextPage ? <ActivityIndicator className="py-4" /> : null}
        renderItem={({ item: sale }) => (
          <SaleRow
            sale={sale}
            timeZone={timeZone}
            currency={currency}
            onPress={() => router.push(`/sale/${sale.id}`)}
          />
        )}
      />
    </View>
  );
}
