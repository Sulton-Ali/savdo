import { Redirect } from "expo-router";
import { useTranslation } from "react-i18next";
import { ActivityIndicator, FlatList, RefreshControl, View } from "react-native";
import { Text } from "@/components/ui/text";
import { formatQty } from "@/features/catalog/qty";
import { useLowStockRows } from "@/features/stock/hooks";
import { useSession } from "@/lib/session";

/**
 * Low stock (T5, split out of the old `stock/index.tsx` section toggle,
 * T12/D-90) — its own drawer item now, reached by pushing `/stock/low`
 * (still nested in the Stock tab's own `_layout.tsx` Stack, so the tab bar
 * stays visible and the URL is unchanged). `GET /stock/low` itself requires
 * `stock.write` (`docs/05-API.md` § Stock, manager+); redirected back to
 * the levels tab rather than shown to a cashier, same gate every other
 * write-only stock screen on this tab uses (`adjust.tsx`, `purchases/*`).
 */
export default function StockLowScreen() {
  const { t } = useTranslation();
  const { can } = useSession();
  const canWrite = can("stock.write");
  const lowStock = useLowStockRows(canWrite);

  if (!canWrite) {
    return <Redirect href="/stock" />;
  }

  return (
    <View className="flex-1 bg-background px-4">
      <FlatList
        className="flex-1"
        contentContainerStyle={{ paddingVertical: 12 }}
        data={lowStock.rows}
        keyExtractor={(item) => item.variantId}
        refreshControl={
          <RefreshControl refreshing={lowStock.isRefetching} onRefresh={() => lowStock.refetch()} />
        }
        onEndReachedThreshold={0.4}
        onEndReached={() => {
          if (lowStock.hasNextPage && !lowStock.isFetchingNextPage) {
            lowStock.fetchNextPage();
          }
        }}
        ListEmptyComponent={
          lowStock.isPending ? (
            <ActivityIndicator className="py-8" />
          ) : (
            <Text variant="muted" className="p-4 text-center">
              {t("mobile.stock.lowEmpty")}
            </Text>
          )
        }
        ListFooterComponent={
          lowStock.isFetchingNextPage ? <ActivityIndicator className="py-4" /> : null
        }
        renderItem={({ item }) => (
          <View className="min-h-12 flex-row items-center justify-between border-border border-b px-1 py-3">
            <View className="flex-1 pr-2">
              <Text numberOfLines={1}>{item.productName}</Text>
              {!!item.variantLabel && <Text variant="muted">{item.variantLabel}</Text>}
            </View>
            <Text variant="muted">
              {t("mobile.stock.lowQty", { qty: formatQty(item.qty), threshold: item.threshold })}
            </Text>
          </View>
        )}
      />
    </View>
  );
}
