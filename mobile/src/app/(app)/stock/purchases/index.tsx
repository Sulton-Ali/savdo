import { Redirect, useRouter } from "expo-router";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { ActivityIndicator, FlatList, Pressable, RefreshControl, View } from "react-native";

import { useLocations } from "@/features/catalog/hooks";
import { usePurchases, useSuppliers } from "@/features/purchases/hooks";

import { Text } from "@/components/ui/text";
import { formatMoney } from "@/lib/money";
import { useSession } from "@/lib/session";

/**
 * Draft purchases (deliverable 3, `stock.write` only) — includes drafts
 * created on the admin web (D-80: "list and receive draft purchases created
 * on the admin web"), since `GET /purchases?status=draft` isn't scoped to
 * the client that created them. Tapping a row pushes to
 * `purchases/[id].tsx`, which offers "Receive".
 */
export default function PurchaseDraftsScreen() {
  const { t } = useTranslation();
  const router = useRouter();
  const { can, shop } = useSession();
  const currency = shop?.currency ?? "UZS";

  const { data: suppliers } = useSuppliers();
  const { data: locations } = useLocations();
  const suppliersById = useMemo(
    () => new Map((suppliers ?? []).map((supplier) => [supplier.id, supplier])),
    [suppliers],
  );
  const locationsById = useMemo(
    () => new Map((locations ?? []).map((location) => [location.id, location])),
    [locations],
  );

  const {
    data,
    isPending,
    isRefetching,
    refetch,
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
  } = usePurchases("draft");
  const purchases = useMemo(() => data?.pages.flatMap((page) => page.items) ?? [], [data]);

  if (!can("stock.write")) {
    return <Redirect href="/stock" />;
  }

  return (
    <View className="flex-1 bg-background">
      <FlatList
        className="flex-1 px-4"
        contentContainerStyle={{ paddingVertical: 12, gap: 8 }}
        data={purchases}
        keyExtractor={(purchase) => purchase.id}
        refreshControl={<RefreshControl refreshing={isRefetching} onRefresh={refetch} />}
        onEndReachedThreshold={0.4}
        onEndReached={() => {
          if (hasNextPage && !isFetchingNextPage) {
            fetchNextPage();
          }
        }}
        ListEmptyComponent={
          isPending ? (
            <ActivityIndicator className="py-8" />
          ) : (
            <Text variant="muted" className="p-4 text-center">
              {t("mobile.purchases.draftsEmpty")}
            </Text>
          )
        }
        ListFooterComponent={isFetchingNextPage ? <ActivityIndicator className="py-4" /> : null}
        renderItem={({ item: purchase }) => (
          <Pressable
            accessibilityRole="button"
            className="min-h-12 flex-row items-center justify-between rounded-md border border-border bg-card p-3 active:bg-accent"
            onPress={() => router.push(`/stock/purchases/${purchase.id}`)}
          >
            <View className="flex-1 pr-2">
              <Text numberOfLines={1}>{purchase.number}</Text>
              <Text variant="muted" numberOfLines={1}>
                {suppliersById.get(purchase.supplierId)?.name ?? "—"} ·{" "}
                {locationsById.get(purchase.locationId)?.name ?? "—"}
              </Text>
            </View>
            <Text>{formatMoney(purchase.totalCost, currency)}</Text>
          </Pressable>
        )}
      />
    </View>
  );
}
