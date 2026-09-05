import { useQueries, useQuery } from "@tanstack/react-query";
import { Search } from "lucide-react-native";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { FlatList, Pressable, TextInput, View } from "react-native";

import { Text } from "@/components/ui/text";
import { formatMoney } from "@/lib/money";
import { catalogKeys } from "@/lib/queryKeys";
import { useSession } from "@/lib/session";

import { listAllStockLevels, listProducts, listVariants, type Product, type Variant } from "./api";
import { useDebouncedValue } from "./hooks";
import { resolveEffectivePrice } from "./pricing";
import { formatQty } from "./qty";

/** Matches every other free-text search in this app (`docs/05-API.md` §
 * Conventions: ILIKE/trigram search over a handful of characters is
 * noisy). */
const MIN_QUERY_LENGTH = 2;
const SEARCH_DEBOUNCE_MS = 300;

interface VariantRow {
  product: Product;
  variant: Variant;
  qty: string;
}

/**
 * Resolves a free-text query into a flat list of matching variants at
 * `locationId`, joining three endpoints client-side: `GET /products?q=`,
 * then per matched product `GET /products/{id}/variants` and `GET
 * /stock/levels?productId&locationId` — the same "list vs get asymmetry"
 * `admin/src/routes/app/StockVariantPicker.tsx` and `StockLevelsPage.tsx`
 * work around, flattened into one search instead of admin's two cascading
 * selects. Bounded by the product search's own page size (50 products),
 * so the fan-out of per-product requests stays small for a single shop's
 * catalogue.
 */
function useVariantSearch(query: string, locationId: string) {
  const trimmed = query.trim();
  const enabled = trimmed.length >= MIN_QUERY_LENGTH;

  const productsQuery = useQuery({
    queryKey: catalogKeys.products({ q: trimmed }),
    queryFn: () => listProducts({ q: trimmed, cursor: null }),
    enabled,
  });
  const products = useMemo(() => productsQuery.data?.items ?? [], [productsQuery.data]);

  const variantQueries = useQueries({
    queries: products.map((product) => ({
      queryKey: catalogKeys.variants(product.id),
      queryFn: () => listVariants(product.id),
    })),
  });
  const stockQueries = useQueries({
    queries: products.map((product) => ({
      queryKey: catalogKeys.stockLevels({ productId: product.id, locationId }),
      queryFn: () => listAllStockLevels({ productId: product.id, locationId }),
    })),
  });

  const rows = useMemo<VariantRow[]>(() => {
    const result: VariantRow[] = [];
    products.forEach((product, index) => {
      const variants = variantQueries[index]?.data ?? [];
      const levels = stockQueries[index]?.data ?? [];
      for (const variant of variants) {
        const level = levels.find((l) => l.variantId === variant.id);
        result.push({ product, variant, qty: level?.qty ?? "0" });
      }
    });
    return result;
  }, [products, variantQueries, stockQueries]);

  const isFetching =
    productsQuery.isFetching ||
    variantQueries.some((q) => q.isFetching) ||
    stockQueries.some((q) => q.isFetching);

  return { rows, isFetching, searched: enabled };
}

function variantLabel(variant: Variant): string {
  return Object.entries(variant.attributes)
    .map(([key, val]) => `${key}: ${val}`)
    .join(", ");
}

export interface VariantPickerProps {
  /** Quantities are shown, and reported to `onPick`, at this location only. */
  locationId: string;
  onPick: (variant: Variant, availableQty: string) => void;
  /** Variant ids to hide from the results, e.g. lines already in a cart. */
  excludeVariantIds?: string[];
}

/**
 * Search-and-pick a variant with its quantity at one location (deliverable
 * 6) — shared, presentational component for the quick-sale cart (T4) and
 * stock adjustment/transfer forms (T5). Owns no navigation: picking a row
 * calls `onPick` and leaves closing any host sheet/modal to the caller.
 * Rows are at least 48dp tall for a till/warehouse touch target.
 */
export function VariantPicker({ locationId, onPick, excludeVariantIds }: VariantPickerProps) {
  const { t } = useTranslation();
  const { shop } = useSession();
  const timeZone = shop?.timezone ?? Intl.DateTimeFormat().resolvedOptions().timeZone;
  const currency = shop?.currency ?? "UZS";

  const [rawQuery, setRawQuery] = useState("");
  const debouncedQuery = useDebouncedValue(rawQuery, SEARCH_DEBOUNCE_MS);
  const { rows, isFetching, searched } = useVariantSearch(debouncedQuery, locationId);

  const excluded = useMemo(() => new Set(excludeVariantIds ?? []), [excludeVariantIds]);
  const visibleRows = useMemo(
    () => rows.filter((row) => !excluded.has(row.variant.id)),
    [rows, excluded],
  );

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
        data={visibleRows}
        keyExtractor={(row) => row.variant.id}
        ListEmptyComponent={
          searched && !isFetching ? (
            <Text variant="muted" className="p-4 text-center">
              {t("mobile.catalog.picker.empty")}
            </Text>
          ) : null
        }
        renderItem={({ item }) => (
          <Pressable
            accessibilityRole="button"
            className="min-h-12 flex-row items-center justify-between border-border border-b px-3 py-3 active:bg-accent"
            onPress={() => onPick(item.variant, item.qty)}
          >
            <View className="flex-1 gap-0.5 pr-2">
              <Text numberOfLines={1}>{item.product.name}</Text>
              <Text variant="muted" numberOfLines={1}>
                {variantLabel(item.variant)}
              </Text>
            </View>
            <View className="items-end gap-0.5">
              <Text>
                {formatMoney(resolveEffectivePrice(item.product, item.variant, timeZone), currency)}
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
