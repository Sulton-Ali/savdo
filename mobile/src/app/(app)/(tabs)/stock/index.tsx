import { useRouter } from "expo-router";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { ActivityIndicator, View } from "react-native";
import { Text } from "@/components/ui/text";
import { useLocations } from "@/features/catalog/hooks";
import { VariantPicker } from "@/features/catalog/VariantPicker";
import { ChipGroup } from "@/features/stock/ChipGroup";
import { getPreferredLocationId, setPreferredLocationId } from "@/features/stock/preferredLocation";
import { useSession } from "@/lib/session";

/**
 * Stock tab home (T5, split T12/D-90): *only* the levels page now — a
 * location selector (remembered across restarts,
 * `features/stock/preferredLocation.ts`) plus the levels list
 * (search-and-browse via the shared `VariantPicker`, every role — D-40).
 * "Low stock", "New purchase"/"Drafts" and "Adjustment" used to live here
 * as a section toggle and a three-button action row; both are gone — those
 * destinations are now drawer items (`components/AppDrawerContent.tsx`)
 * that push straight to `stock/low.tsx`/`stock/purchases`/`stock/adjust.tsx`
 * (still nested in this same tab's `_layout.tsx` Stack, so their URLs are
 * unchanged).
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
      </View>

      <View className="mt-3 flex-1 px-4">
        {locationId ? (
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
