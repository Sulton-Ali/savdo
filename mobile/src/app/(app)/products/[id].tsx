import { useLocalSearchParams, useRouter } from "expo-router";
import { ArrowLeft } from "lucide-react-native";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { ActivityIndicator, Image, Pressable, ScrollView, View } from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";

import { Text } from "@/components/ui/text";
import type { Variant } from "@/features/catalog/api";
import {
  useLocations,
  useMediaUrl,
  useProduct,
  useVariantsWithStock,
} from "@/features/catalog/hooks";
import { isPromoActive, resolveEffectivePrice } from "@/features/catalog/pricing";
import { formatQty } from "@/features/catalog/qty";
import { formatMoney } from "@/lib/money";
import { useSession } from "@/lib/session";

function variantAttributesLabel(variant: Variant): string {
  return (
    Object.entries(variant.attributes)
      .map(([key, val]) => `${key}: ${val}`)
      .join(", ") ||
    variant.sku ||
    ""
  );
}

/**
 * Product detail (deliverable 4): cover image, price (promo price struck
 * through against the regular price when active, D-67/D-68), and a
 * variants × locations grid — every role can view (`docs/04-DATA-MODEL.md`
 * § 7); manager/owner additionally get an Edit entry to the placeholder at
 * `products/[id]/edit` (T3 fills it in). This screen (and its sibling
 * `edit.tsx`) are declared as hidden extra screens of the existing
 * `(app)/_layout.tsx` Products tab (`options={{ href: null }}`) rather than
 * a nested Stack, so they render their own back button here instead of
 * relying on a native header — see this task's report for why.
 */
export default function ProductDetailScreen() {
  const { t } = useTranslation();
  const router = useRouter();
  const { id } = useLocalSearchParams<{ id: string }>();
  const { role, shop } = useSession();
  const canEdit = role === "owner" || role === "manager";
  const timeZone = shop?.timezone ?? Intl.DateTimeFormat().resolvedOptions().timeZone;
  const currency = shop?.currency ?? "UZS";

  const productQuery = useProduct(id);
  const { variants, isLoading: variantsLoading } = useVariantsWithStock(productQuery.data?.id);
  const { data: locations } = useLocations();
  const activeLocations = useMemo(() => (locations ?? []).filter((l) => l.isActive), [locations]);
  // Every hook must run on every render regardless of the early returns
  // below (Rules of Hooks), so this is derived here, before them, even
  // though `cover`/`coverUrl` are only rendered once `productQuery.data`
  // exists.
  const cover =
    productQuery.data?.images?.find((image) => image.isCover) ?? productQuery.data?.images?.[0];
  const coverUrl = useMediaUrl(cover?.urls.card);

  if (productQuery.isPending) {
    return (
      <SafeAreaView edges={["top"]} className="flex-1 items-center justify-center bg-background">
        <ActivityIndicator />
      </SafeAreaView>
    );
  }

  if (productQuery.isError || !productQuery.data) {
    return (
      <SafeAreaView
        edges={["top"]}
        className="flex-1 items-center justify-center gap-2 bg-background p-6"
      >
        <Text variant="muted">{t("errors.notFound")}</Text>
        <Pressable accessibilityRole="button" onPress={() => router.back()}>
          <Text className="text-primary">{t("common.back")}</Text>
        </Pressable>
      </SafeAreaView>
    );
  }

  const product = productQuery.data;
  const promoActive = isPromoActive(product, timeZone, new Date());

  return (
    <SafeAreaView edges={["top"]} className="flex-1 bg-background">
      <View className="min-h-12 flex-row items-center gap-3 border-border border-b px-2 py-2">
        <Pressable
          accessibilityRole="button"
          accessibilityLabel={t("common.back")}
          className="h-12 w-12 items-center justify-center"
          onPress={() => router.back()}
        >
          <ArrowLeft size={22} />
        </Pressable>
        <Text variant="h4" numberOfLines={1} className="flex-1">
          {product.name}
        </Text>
        {canEdit && (
          <Pressable
            accessibilityRole="button"
            className="min-h-12 justify-center px-3"
            onPress={() => router.push(`/products/${product.id}/edit`)}
          >
            <Text className="text-primary">{t("catalog.products.edit")}</Text>
          </Pressable>
        )}
      </View>

      <ScrollView contentContainerStyle={{ padding: 16, gap: 16 }}>
        {coverUrl ? (
          <Image
            source={{ uri: coverUrl }}
            style={{ width: "100%", aspectRatio: 1, borderRadius: 8 }}
            resizeMode="cover"
          />
        ) : null}

        <View className="gap-1">
          {promoActive && product.promoPrice != null ? (
            <View className="flex-row items-baseline gap-2">
              <Text variant="h3">{formatMoney(product.promoPrice, currency)}</Text>
              <Text variant="muted" className="line-through">
                {formatMoney(product.basePrice, currency)}
              </Text>
            </View>
          ) : (
            <Text variant="h3">{formatMoney(product.basePrice, currency)}</Text>
          )}
        </View>

        <View className="gap-2">
          <Text variant="large">{t("mobile.catalog.detail.variantsTitle")}</Text>

          {variantsLoading ? (
            <ActivityIndicator />
          ) : (
            <ScrollView horizontal showsHorizontalScrollIndicator={false}>
              <View>
                <View className="flex-row border-border border-b pb-2">
                  <Text variant="small" className="w-36">
                    {t("mobile.catalog.detail.columns.variant")}
                  </Text>
                  <Text variant="small" className="w-24">
                    {t("mobile.catalog.detail.columns.price")}
                  </Text>
                  {activeLocations.map((location) => (
                    <Text key={location.id} variant="small" className="w-20" numberOfLines={1}>
                      {location.name}
                    </Text>
                  ))}
                </View>

                {variants.map(({ variant, qtyByLocation }) => (
                  <View
                    key={variant.id}
                    className="flex-row items-center border-border border-b py-2"
                  >
                    <Text className="w-36" numberOfLines={2}>
                      {variantAttributesLabel(variant)}
                    </Text>
                    <Text className="w-24">
                      {formatMoney(resolveEffectivePrice(product, variant, timeZone), currency)}
                    </Text>
                    {activeLocations.map((location) => (
                      <Text key={location.id} className="w-20">
                        {formatQty(qtyByLocation[location.id])}
                      </Text>
                    ))}
                  </View>
                ))}
              </View>
            </ScrollView>
          )}
        </View>
      </ScrollView>
    </SafeAreaView>
  );
}
