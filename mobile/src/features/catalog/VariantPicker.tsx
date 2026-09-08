import { useQuery } from "@tanstack/react-query";
import { ArrowLeft, Search } from "lucide-react-native";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { ActivityIndicator, FlatList, Pressable, TextInput, View } from "react-native";

import { Text } from "@/components/ui/text";
import { formatMoney } from "@/lib/money";
import { catalogKeys } from "@/lib/queryKeys";
import { useSession } from "@/lib/session";

import { listAllStockLevels, listVariants, type Product, type Variant } from "./api";
import { useDebouncedValue, useProductsSearch } from "./hooks";
import { resolveEffectivePrice } from "./pricing";
import { flattenProductPages } from "./productPages";
import { formatQty } from "./qty";

const SEARCH_DEBOUNCE_MS = 300;

function variantLabel(variant: Variant): string {
  return Object.entries(variant.attributes)
    .map(([key, val]) => `${key}: ${val}`)
    .join(", ");
}

interface VariantRow {
  variant: Variant;
  qty: string;
}

export interface VariantPickerProps {
  /** Quantities are shown, and reported to `onPick`, at this location only. */
  locationId: string;
  /** `product` is the variant's parent — additive third argument (T4 review):
   * a caller that only destructures `(variant, availableQty)` keeps working
   * unchanged (extra JS callback arguments are simply ignored), so T5's
   * stock adjustment/transfer pickers needed no update for this. Lets a
   * caller resolve a display price (`resolveEffectivePrice`) or product
   * name without a second fetch — see `features/sales/cart.ts`'s `CartLine`. */
  onPick: (variant: Variant, availableQty: string, product: Product) => void;
  /** Variant ids to hide from the results, e.g. lines already in a cart. */
  excludeVariantIds?: string[];
}

/**
 * Search-and-pick a variant with its quantity at one location (deliverable
 * 6) — shared, presentational component for the quick-sale cart (T4) and
 * stock adjustment/transfer forms (T5). Two steps, mirroring the admin
 * precedent (`admin/src/routes/app/StockVariantPicker.tsx`) rather than a
 * per-keystroke fan-out across every matching product:
 *
 * 1. Search products by name (one debounced `GET /products?q=` call).
 * 2. Tap a product to load its variants and its stock at `locationId`
 *    (two calls, `GET /products/{id}/variants` and `GET
 *    /stock/levels?productId&locationId`), then pick one of its variant
 *    rows — a "Back" row returns to the step-1 results without re-querying
 *    them (still cached under the same query key).
 *
 * Owns no navigation: picking a row calls `onPick` and leaves closing any
 * host sheet/modal to the caller. Rows are at least 48dp tall for a
 * till/warehouse touch target.
 */
export function VariantPicker({ locationId, onPick, excludeVariantIds }: VariantPickerProps) {
  const { t } = useTranslation();
  const { shop } = useSession();
  const timeZone = shop?.timezone ?? Intl.DateTimeFormat().resolvedOptions().timeZone;
  const currency = shop?.currency ?? "UZS";

  const [rawQuery, setRawQuery] = useState("");
  const debouncedQuery = useDebouncedValue(rawQuery, SEARCH_DEBOUNCE_MS);
  const trimmedQuery = debouncedQuery.trim();

  const [selectedProduct, setSelectedProduct] = useState<Product | null>(null);

  // D-92: queries with an empty `q` as soon as this mounts, so the newest
  // products show immediately (`listProducts` already treats `q: ""` as
  // "no filter" — `{ q: q || undefined }`, same as `products/index.tsx`
  // and `customers/index.tsx`'s own search screens); typing narrows the
  // results via the debounced query above. No minimum query length either
  // (dropped, was 2 chars) — D-92 explicitly asks for this.
  //
  // Reuses `useProductsSearch`, the same `useInfiniteQuery` the Products
  // tab uses, rather than a second `useQuery` under the same
  // `catalogKeys.products` key: TanStack Query caches one entry per key, so
  // a second consumer expecting a different shape (`{ items }` instead of
  // `{ pages, pageParams }`) read whichever shape the *other* screen had
  // last cached and saw an empty list — the bug the owner reported on this
  // picker (stock levels, adjustment, quick sale, new purchase all use it).
  const productsQuery = useProductsSearch(trimmedQuery);
  const products = useMemo(
    () => flattenProductPages(productsQuery.data?.pages),
    [productsQuery.data],
  );

  const variantsQuery = useQuery({
    queryKey: catalogKeys.variants(selectedProduct?.id ?? ""),
    queryFn: () => listVariants(selectedProduct?.id as string),
    enabled: selectedProduct != null,
  });
  const stockQuery = useQuery({
    queryKey: catalogKeys.stockLevels({ productId: selectedProduct?.id, locationId }),
    queryFn: () => listAllStockLevels({ productId: selectedProduct?.id, locationId }),
    enabled: selectedProduct != null,
  });

  const excluded = useMemo(() => new Set(excludeVariantIds ?? []), [excludeVariantIds]);
  const variantRows = useMemo<VariantRow[]>(() => {
    const levels = stockQuery.data ?? [];
    return (variantsQuery.data ?? [])
      .filter((variant) => !excluded.has(variant.id))
      .map((variant) => ({
        variant,
        qty: levels.find((level) => level.variantId === variant.id)?.qty ?? "0",
      }));
  }, [variantsQuery.data, stockQuery.data, excluded]);

  if (selectedProduct) {
    return (
      <View className="flex-1 gap-2">
        <Pressable
          accessibilityRole="button"
          className="min-h-12 flex-row items-center gap-2 px-1"
          onPress={() => setSelectedProduct(null)}
        >
          <ArrowLeft size={18} />
          <Text numberOfLines={1} className="flex-1 font-medium">
            {selectedProduct.name}
          </Text>
        </Pressable>
        <FlatList
          data={variantRows}
          keyExtractor={(row) => row.variant.id}
          ListEmptyComponent={
            variantsQuery.isFetching || stockQuery.isFetching ? null : (
              <Text variant="muted" className="p-4 text-center">
                {t("mobile.catalog.picker.empty")}
              </Text>
            )
          }
          renderItem={({ item }) => (
            <Pressable
              accessibilityRole="button"
              className="min-h-12 flex-row items-center justify-between border-border border-b px-3 py-3 active:bg-accent"
              onPress={() => onPick(item.variant, item.qty, selectedProduct)}
            >
              <Text numberOfLines={1} className="flex-1 pr-2">
                {variantLabel(item.variant)}
              </Text>
              <View className="items-end gap-0.5">
                <Text>
                  {formatMoney(
                    resolveEffectivePrice(selectedProduct, item.variant, timeZone),
                    currency,
                  )}
                </Text>
                <Text variant="muted">
                  {t("mobile.catalog.picker.qtyAvailable", { qty: formatQty(item.qty) })}
                </Text>
              </View>
            </Pressable>
          )}
        />
      </View>
    );
  }

  return (
    <View className="flex-1 gap-2">
      <View className="h-12 flex-row items-center gap-2 rounded-md border border-input bg-background px-3">
        <Search color="#71717a" size={18} />
        <TextInput
          className="flex-1 text-base text-foreground"
          placeholder={t("mobile.catalog.picker.searchPlaceholder")}
          value={rawQuery}
          onChangeText={setRawQuery}
          autoCorrect={false}
          accessibilityLabel={t("mobile.catalog.picker.searchPlaceholder")}
        />
      </View>
      <FlatList
        data={products}
        keyExtractor={(product) => product.id}
        onEndReachedThreshold={0.4}
        onEndReached={() => {
          if (productsQuery.hasNextPage && !productsQuery.isFetchingNextPage) {
            productsQuery.fetchNextPage();
          }
        }}
        ListEmptyComponent={
          !productsQuery.isFetching ? (
            <Text variant="muted" className="p-4 text-center">
              {t("mobile.catalog.picker.empty")}
            </Text>
          ) : null
        }
        ListFooterComponent={
          productsQuery.isFetchingNextPage ? <ActivityIndicator className="py-4" /> : null
        }
        renderItem={({ item: product }) => (
          <Pressable
            accessibilityRole="button"
            className="min-h-12 justify-center border-border border-b px-3 py-3 active:bg-accent"
            onPress={() => setSelectedProduct(product)}
          >
            <Text numberOfLines={1}>{product.name}</Text>
          </Pressable>
        )}
      />
    </View>
  );
}
