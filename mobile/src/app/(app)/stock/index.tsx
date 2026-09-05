import { useRouter } from "expo-router";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { ActivityIndicator, FlatList, RefreshControl, View } from "react-native";

import { VariantPicker } from "@/features/catalog/VariantPicker";
import { useLocations } from "@/features/catalog/hooks";
import { formatQty } from "@/features/catalog/qty";
import { ChipGroup } from "@/features/stock/ChipGroup";
import { useLowStockRows } from "@/features/stock/hooks";
import {
  getPreferredLocationId,
  setPreferredLocationId,
} from "@/features/stock/preferredLocation";
import { Button } from "@/components/ui/button";
import { Text } from "@/components/ui/text";
import { useSession } from "@/lib/session";

type Section = "levels" | "low";

/**
 * Stock tab home (deliverable 3): a location selector (remembered across
 * restarts, `features/stock/preferredLocation.ts`), the levels list
 * (search-and-browse via the shared `VariantPicker`, every role — D-40) and,
 * for `stock.write` only, a "Low stock" section plus the "New purchase" /
 * "Drafts" / "Adjustment" actions.
 *
 * "Low stock" is gated on `stock.write`, not shown read-only to every role:
 * `GET /stock/low` itself requires `stock.write` (`docs/05-API.md` § Stock
 * lists it as `manager+`; `api/internal/stock/low.go` calls
 * `auth.Require(ctx, auth.PermStockWrite)`) — this task's brief says "every
 * role sees ... the low-stock list", which the API contract and the
 * existing permission matrix both disagree with; flagged in this task's
 * report rather than guessed past.
 */
export default function StockScreen() {
  const { t } = useTranslation();
  const router = useRouter();
  const { can } = useSession();
  const canWrite = can("stock.write");

  const locationsQuery = useLocations();
  const activeLocations = useMemo(
    () => (locationsQuery.data ?? []).filter((location) => location.isActive),
    [locationsQuery.data],
  );

  const [locationId, setLocationId] = useState<string | null>(null);
  const [preferenceLoaded, setPreferenceLoaded] = useState(false);

  useEffect(() => {
    let cancelled = false;
    getPreferredLocationId().then((stored) => {
      if (!cancelled) {
        setLocationId(stored);
        setPreferenceLoaded(true);
      }
    });
    return () => {
      cancelled = true;
    };
  }, []);

  // Once locations have loaded, fall back to the shop's default location
  // (else the first active one) whenever there's no remembered choice, or
  // the remembered one no longer exists/is inactive.
  useEffect(() => {
    if (!preferenceLoaded || activeLocations.length === 0) {
      return;
    }
    const stillValid = locationId != null && activeLocations.some((l) => l.id === locationId);
    if (stillValid) {
      return;
    }
    const fallback = activeLocations.find((l) => l.isDefault) ?? activeLocations[0];
    if (fallback) {
      setLocationId(fallback.id);
    }
  }, [preferenceLoaded, activeLocations, locationId]);

  function handleSelectLocation(id: string) {
    setLocationId(id);
    void setPreferredLocationId(id);
  }

  const [section, setSection] = useState<Section>("levels");
  const lowStock = useLowStockRows(canWrite && section === "low");

  return (
    <View className="flex-1 bg-background">
      <View className="gap-3 px-4 pt-4">
        <View className="gap-1.5">
          <Text variant="small">{t("stock.fields.location")}</Text>
          {locationsQuery.isPending ? (
            <ActivityIndicator />
          ) : (
            <ChipGroup
              accessibilityLabel={t("stock.fields.location")}
              options={activeLocations.map((location) => ({
                value: location.id,
                label: location.name,
              }))}
              value={locationId}
              onChange={handleSelectLocation}
            />
          )}
        </View>

        {canWrite && (
          <View className="flex-row flex-wrap gap-2">
            <Button size="sm" onPress={() => router.push("/stock/purchases/new")}>
              <Text>{t("purchases.add")}</Text>
            </Button>
            <Button variant="outline" size="sm" onPress={() => router.push("/stock/purchases")}>
              <Text>{t("mobile.purchases.drafts")}</Text>
            </Button>
            <Button variant="outline" size="sm" onPress={() => router.push("/stock/adjust")}>
              <Text>{t("stock.actions.adjust")}</Text>
            </Button>
          </View>
        )}

        {canWrite && (
          <View className="flex-row gap-2">
            <Button
              variant={section === "levels" ? "default" : "outline"}
              size="sm"
              onPress={() => setSection("levels")}
            >
              <Text>{t("stock.levels.title")}</Text>
            </Button>
            <Button
              variant={section === "low" ? "default" : "outline"}
              size="sm"
              onPress={() => setSection("low")}
            >
              <Text>{t("stock.low.title")}</Text>
            </Button>
          </View>
        )}
      </View>

      <View className="mt-3 flex-1 px-4">
        {section === "low" && canWrite ? (
          <FlatList
            data={lowStock.rows}
            keyExtractor={(item) => item.variantId}
            refreshControl={
              <RefreshControl
                refreshing={lowStock.isRefetching}
                onRefresh={() => lowStock.refetch()}
              />
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
                  {t("mobile.stock.lowQty", {
                    qty: formatQty(item.qty),
                    threshold: item.threshold,
                  })}
                </Text>
              </View>
            )}
          />
        ) : locationId ? (
          <VariantPicker
            locationId={locationId}
            onPick={(variant) => {
              if (canWrite) {
                router.push(`/stock/variant/${variant.id}`);
              }
            }}
          />
        ) : (
          <View className="flex-1 items-center justify-center">
            <Text variant="muted">{t("mobile.stock.noLocations")}</Text>
          </View>
        )}
      </View>
    </View>
  );
}
