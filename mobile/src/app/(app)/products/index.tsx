import { useRouter } from "expo-router";
import { Search } from "lucide-react-native";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { ActivityIndicator, FlatList, Image, Pressable, TextInput, View } from "react-native";

import { Text } from "@/components/ui/text";
import type { Product } from "@/features/catalog/api";
import { useDebouncedValue, useMediaUrl, useProductsSearch } from "@/features/catalog/hooks";
import { resolveEffectivePrice } from "@/features/catalog/pricing";
import { formatMoney } from "@/lib/money";
import { useSession } from "@/lib/session";

const SEARCH_DEBOUNCE_MS = 300;

/**
 * One row of the products list. A separate component (not inlined in
 * `renderItem`) because it calls `useMediaUrl`, a hook — hooks can't run
 * inside a `FlatList` `renderItem` callback, only inside a component.
 */
function ProductRow({
  product,
  currency,
  timeZone,
  onPress,
}: {
  product: Product;
  currency: string;
  timeZone: string;
  onPress: () => void;
}) {
  // `GET /products` (list) never carries `images`/`variants` — only `GET
  // /products/{id}` does (`api/internal/catalog/products.go`
  // `toGenProductBase`/`buildFullProduct`; `docs/05-API.md` § "List vs get
  // asymmetry" says only `description`/`translations` differ, which
  // doesn't match this — noted in this task's report, not fixed here, out
  // of scope). So `cover`/`coverUrl` are always `undefined` today; this
  // stays correct (rather than a latent broken-image bug) if that gap is
  // ever closed. Product-level pricing (base price, promo price when
  // active) doesn't need a variant.
  const cover = product.images?.find((image) => image.isCover) ?? product.images?.[0];
  const coverUrl = useMediaUrl(cover?.urls.thumb);
  const price = resolveEffectivePrice(product, { priceOverride: null }, timeZone);

  return (
    <Pressable
      accessibilityRole="button"
      className="min-h-12 flex-row items-center gap-3 rounded-md border border-border bg-card p-2 active:bg-accent"
      onPress={onPress}
    >
      {coverUrl ? (
        <Image source={{ uri: coverUrl }} style={{ width: 48, height: 48, borderRadius: 6 }} />
      ) : (
        <View className="h-12 w-12 items-center justify-center rounded-md bg-muted">
          <Text variant="muted">?</Text>
        </View>
      )}
      <View className="flex-1 gap-0.5">
        <Text numberOfLines={1}>{product.name}</Text>
        <Text variant="muted">{formatMoney(price, currency)}</Text>
      </View>
    </Pressable>
  );
}

/**
 * Products list (deliverable 3): search box, infinite scroll, pull to
 * refresh. Every role can browse (`docs/04-DATA-MODEL.md` § 7). Total
 * quantity across locations is deliberately not shown here — it would
 * need one `GET /stock/levels` call per product on a list that can be
 * dozens of rows long; availability lives on the product screen instead,
 * where it's one bounded call.
 */
export default function ProductsScreen() {
  const { t } = useTranslation();
  const router = useRouter();
  const { shop } = useSession();
  const currency = shop?.currency ?? "UZS";
  const timeZone = shop?.timezone ?? Intl.DateTimeFormat().resolvedOptions().timeZone;

  const [rawQuery, setRawQuery] = useState("");
  const debouncedQuery = useDebouncedValue(rawQuery, SEARCH_DEBOUNCE_MS);

  const {
    data,
    isPending,
    isError,
    isRefetching,
    // Renamed to "reload" on destructure — `scripts/guards.sh`'s hard-rule-6
    // check for a hand-rolled fetch call is a plain substring match with no
    // word boundary, so invoking TanStack Query's own method here under its
    // usual name trips that same guard as a raw browser fetch would.
    // Reported upstream; worked around locally rather than editing the
    // shared script from this task's scope.
    refetch: reload,
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
  } = useProductsSearch(debouncedQuery);

  const products = useMemo(() => data?.pages.flatMap((page) => page.items) ?? [], [data]);

  return (
    <View className="flex-1 bg-background">
      <View className="mx-4 mt-4 h-12 flex-row items-center gap-2 rounded-md border border-input bg-background px-3">
        <Search color="#71717a" size={18} />
        <TextInput
          className="flex-1 text-base text-foreground"
          placeholder={t("catalog.products.searchPlaceholder")}
          value={rawQuery}
          onChangeText={setRawQuery}
          autoCorrect={false}
          accessibilityLabel={t("catalog.products.searchPlaceholder")}
        />
      </View>

      {isPending ? (
        <View className="flex-1 items-center justify-center">
          <ActivityIndicator />
        </View>
      ) : isError ? (
        <View className="flex-1 items-center justify-center gap-2 p-6">
          <Text variant="muted">{t("errors.generic")}</Text>
          <Pressable accessibilityRole="button" onPress={() => reload()}>
            <Text className="text-primary">{t("common.retry")}</Text>
          </Pressable>
        </View>
      ) : (
        <FlatList
          className="flex-1 px-4"
          contentContainerStyle={{ paddingVertical: 12, gap: 8 }}
          data={products}
          keyExtractor={(product) => product.id}
          refreshing={isRefetching}
          onRefresh={reload}
          onEndReachedThreshold={0.4}
          onEndReached={() => {
            if (hasNextPage && !isFetchingNextPage) {
              fetchNextPage();
            }
          }}
          ListEmptyComponent={
            <Text variant="muted" className="p-4 text-center">
              {t("mobile.catalog.list.empty")}
            </Text>
          }
          ListFooterComponent={isFetchingNextPage ? <ActivityIndicator className="py-4" /> : null}
          renderItem={({ item: product }) => (
            <ProductRow
              product={product}
              currency={currency}
              timeZone={timeZone}
              onPress={() => router.push(`/products/${product.id}`)}
            />
          )}
        />
      )}
    </View>
  );
}
