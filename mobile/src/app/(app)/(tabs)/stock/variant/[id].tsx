import { useQuery } from "@tanstack/react-query";
import { Redirect, useLocalSearchParams } from "expo-router";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { ActivityIndicator, FlatList, View } from "react-native";
import { Text } from "@/components/ui/text";
import { getProduct } from "@/features/catalog/api";
import { useLocations } from "@/features/catalog/hooks";
import { formatQty } from "@/features/catalog/qty";
import { useVariantMovements, useVariantStockLevels } from "@/features/stock/hooks";
import { catalogKeys } from "@/lib/queryKeys";
import { useSession } from "@/lib/session";

/**
 * Optional variant detail (deliverable 3: "movements history if cheap"),
 * reachable from the Stock tab's levels list — `stock.write` only, since
 * `GET /stock/movements` requires it (same gate as every other write route
 * on this tab: redirect back rather than a guaranteed `403`). Shows the
 * variant's per-location levels (`GET /stock/levels?variantId=`, already
 * fetched for the levels list) and its movement ledger
 * (`GET /stock/movements?variantId=`).
 */
export default function VariantMovementsScreen() {
  const { t } = useTranslation();
  const { can } = useSession();
  const { id } = useLocalSearchParams<{ id: string }>();

  const levelsQuery = useVariantStockLevels(id);
  // `StockLevel` carries `productId` (unlike the bare `Variant` the levels
  // list's `VariantPicker` passed here) — resolved from the first level row
  // rather than needing a second endpoint.
  const productId = levelsQuery.data?.[0]?.productId;
  const productQuery = useQuery({
    queryKey: catalogKeys.product(productId ?? ""),
    queryFn: () => getProduct(productId as string),
    enabled: productId != null,
  });
  const variant = productQuery.data?.variants?.find((candidate) => candidate.id === id);
  const variantLabel = variant
    ? Object.entries(variant.attributes)
        .map(([key, val]) => `${key}: ${val}`)
        .join(", ") ||
      variant.sku ||
      ""
    : "";

  const { data: locations } = useLocations();
  const locationsById = useMemo(
    () => new Map((locations ?? []).map((location) => [location.id, location.name])),
    [locations],
  );

  const movementsQuery = useVariantMovements(id);
  const movements = useMemo(
    () => movementsQuery.data?.pages.flatMap((page) => page.items) ?? [],
    [movementsQuery.data],
  );

  if (!can("stock.write")) {
    return <Redirect href="/stock" />;
  }

  return (
    <View className="flex-1 bg-background p-4">
      <Text variant="h4" numberOfLines={1}>
        {productQuery.data?.name ?? "…"}
      </Text>
      {!!variantLabel && <Text variant="muted">{variantLabel}</Text>}

      <View className="mt-3 gap-1">
        <Text variant="small">{t("stock.levels.title")}</Text>
        {(levelsQuery.data ?? []).map((level) => (
          <View key={level.locationId} className="flex-row justify-between">
            <Text variant="muted">{locationsById.get(level.locationId) ?? level.locationId}</Text>
            <Text>{formatQty(level.qty)}</Text>
          </View>
        ))}
      </View>

      <Text variant="small" className="mt-4">
        {t("stock.movements.title")}
      </Text>
      <FlatList
        className="mt-1 flex-1"
        data={movements}
        keyExtractor={(movement) => movement.id}
        onEndReachedThreshold={0.4}
        onEndReached={() => {
          if (movementsQuery.hasNextPage && !movementsQuery.isFetchingNextPage) {
            movementsQuery.fetchNextPage();
          }
        }}
        ListEmptyComponent={
          movementsQuery.isPending ? (
            <ActivityIndicator className="py-8" />
          ) : (
            <Text variant="muted" className="py-4 text-center">
              {t("mobile.stock.movementsEmpty")}
            </Text>
          )
        }
        ListFooterComponent={
          movementsQuery.isFetchingNextPage ? <ActivityIndicator className="py-4" /> : null
        }
        renderItem={({ item: movement }) => (
          <View className="min-h-12 flex-row items-center justify-between border-border border-b py-3">
            <View className="flex-1 pr-2">
              <Text numberOfLines={1}>{t(`stock.movementKinds.${movement.kind}`)}</Text>
              <Text variant="muted" numberOfLines={1}>
                {new Date(movement.createdAt).toLocaleString()}
              </Text>
            </View>
            <Text>{formatQty(movement.qty)}</Text>
          </View>
        )}
      />
    </View>
  );
}
