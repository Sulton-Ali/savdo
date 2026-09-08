import { useLocalSearchParams, useRouter } from "expo-router";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { ActivityIndicator, FlatList, Pressable, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";

import { Text } from "@/components/ui/text";
import { useCustomer, useCustomerSales } from "@/features/customers/hooks";
import type { SaleSummary } from "@/features/sales/api";
import { formatMoney } from "@/lib/money";
import { useSession } from "@/lib/session";

function SaleHistoryRow({
  sale,
  currency,
  onPress,
}: {
  sale: SaleSummary;
  currency: string;
  onPress: () => void;
}) {
  const { t } = useTranslation();
  return (
    <Pressable
      accessibilityRole="button"
      className="min-h-12 flex-row items-center justify-between rounded-md border border-border bg-card p-3 active:bg-accent"
      onPress={onPress}
    >
      <View className="flex-1 gap-0.5 pr-2">
        <Text numberOfLines={1}>
          {t("customers.history.columns.number")} {sale.number}
        </Text>
        <Text variant="muted" numberOfLines={1}>
          {t(`customers.history.kind.${sale.kind}`)} ·{" "}
          {t(`customers.history.paymentMethod.${sale.paymentMethod}`)}
        </Text>
      </View>
      <Text variant="large">{formatMoney(sale.total, currency)}</Text>
    </Pressable>
  );
}

/**
 * Customer detail + purchase history (T4 deliverable 3): `GET
 * /customers/{id}` plus `GET /sales?customerId=` — any `cashier+` may view
 * (D-63: the sales side of this is not limited to the cashier's own
 * sales). Read-only; editing a customer stays admin-web scope for Phase 5
 * (not asked for here). `insets.bottom` (D-95) pads the list's bottom —
 * the Customers stack moved out of the bottom-tabs group in T12 (D-90, now
 * a drawer item), so its screens no longer sit above the tab bar's own
 * safe-area buffer and can reach the true bottom of the display.
 */
export default function CustomerDetailScreen() {
  const { t } = useTranslation();
  const router = useRouter();
  const insets = useSafeAreaInsets();
  const { id } = useLocalSearchParams<{ id: string }>();
  const { shop } = useSession();
  const currency = shop?.currency ?? "UZS";

  const customerQuery = useCustomer(id);
  const salesQuery = useCustomerSales(id);
  const sales = useMemo(
    () => salesQuery.data?.pages.flatMap((page) => page.items) ?? [],
    [salesQuery.data],
  );

  if (customerQuery.isPending) {
    return (
      <View className="flex-1 items-center justify-center bg-background">
        <ActivityIndicator />
      </View>
    );
  }

  if (customerQuery.isError || !customerQuery.data) {
    return (
      <View className="flex-1 items-center justify-center gap-2 bg-background p-6">
        <Text variant="muted">{t("customers.detail.notFound")}</Text>
        <Pressable accessibilityRole="button" onPress={() => router.back()}>
          <Text className="text-primary">{t("common.back")}</Text>
        </Pressable>
      </View>
    );
  }

  const customer = customerQuery.data;

  return (
    <FlatList
      className="flex-1 bg-background"
      contentContainerStyle={{ padding: 16, paddingBottom: insets.bottom + 16, gap: 12 }}
      data={sales}
      keyExtractor={(sale) => sale.id}
      onEndReachedThreshold={0.4}
      onEndReached={() => {
        if (salesQuery.hasNextPage && !salesQuery.isFetchingNextPage) {
          salesQuery.fetchNextPage();
        }
      }}
      ListHeaderComponent={
        <View className="gap-3 pb-2">
          <View className="gap-1">
            <Text variant="h3">{customer.fullName}</Text>
            {customer.phone ? <Text variant="muted">{customer.phone}</Text> : null}
            {customer.telegramUsername ? (
              <Text variant="muted">@{customer.telegramUsername}</Text>
            ) : null}
            {customer.note ? <Text variant="muted">{customer.note}</Text> : null}
          </View>
          <Text variant="large">{t("customers.history.title")}</Text>
        </View>
      }
      ListEmptyComponent={
        !salesQuery.isPending ? (
          <Text variant="muted" className="p-4 text-center">
            {t("customers.history.empty")}
          </Text>
        ) : null
      }
      ListFooterComponent={
        salesQuery.isFetchingNextPage ? <ActivityIndicator className="py-4" /> : null
      }
      renderItem={({ item: sale }) => (
        <SaleHistoryRow
          sale={sale}
          currency={currency}
          onPress={() => router.push(`/sale/${sale.id}`)}
        />
      )}
    />
  );
}
