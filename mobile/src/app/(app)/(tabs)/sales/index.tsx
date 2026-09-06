import { useRouter } from "expo-router";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { ActivityIndicator, FlatList, Pressable, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";

import { Input } from "@/components/ui/input";
import { Text } from "@/components/ui/text";
import { useDebouncedValue } from "@/features/catalog/hooks";
import type { SaleSummary } from "@/features/sales/api";
import {
  DATE_RANGE_PRESETS,
  type DateRangePreset,
  isValidRangeInput,
  presetRange,
} from "@/features/sales/dateRange";
import { useSales } from "@/features/sales/hooks";
import { formatMoney } from "@/lib/money";
import { useSession } from "@/lib/session";

const RANGE_DEBOUNCE_MS = 300;

function SaleRow({
  sale,
  timeZone,
  currency,
  onPress,
}: {
  sale: SaleSummary;
  timeZone: string;
  currency: string;
  onPress: () => void;
}) {
  const { t } = useTranslation();
  const time = new Intl.DateTimeFormat(undefined, {
    timeZone,
    hour: "2-digit",
    minute: "2-digit",
  }).format(new Date(sale.completedAt));

  return (
    <Pressable
      accessibilityRole="button"
      className="min-h-14 gap-1 rounded-md border border-border bg-card p-3 active:bg-accent"
      onPress={onPress}
    >
      <View className="flex-row items-center justify-between">
        <Text>
          {t("sales.columns.number")} {sale.number} · {time}
        </Text>
        <Text variant="large">{formatMoney(sale.total, currency)}</Text>
      </View>
      <View className="flex-row items-center justify-between">
        <Text variant="muted" numberOfLines={1}>
          {sale.customerName ?? t("mobile.sale.customer.none")}
        </Text>
        <Text variant="muted">
          {t(`sales.kind.${sale.kind}`)} · {t(`sales.paymentMethod.${sale.paymentMethod}`)}
          {sale.status === "voided" ? ` · ${t("sales.status.voided")}` : ""}
        </Text>
      </View>
    </Pressable>
  );
}

/**
 * The "Sales list" tab (T12/D-91, promoted from the old `sale/list.tsx`
 * push screen): completed sales newest first (the server orders `GET
 * /sales` that way), defaulting to today, with a preset row (Today,
 * Yesterday, Last 7 days, This month — `features/sales/dateRange.ts`) plus
 * two editable `YYYY-MM-DD` fields for a custom range. A preset button
 * fills both fields with its computed range; typing a custom range clears
 * whichever preset no longer matches it (a preset button is highlighted
 * only when the current fields exactly equal its computed range — derived
 * each render, not tracked as separate state). An invalid or reversed
 * range shows a validation message and the list query stays disabled
 * (D-63 lets every `cashier+` role see every sale for the whole shop and
 * any day, so there is no permission gate here, unlike the old "today
 * only" scoping this replaces).
 */
export default function SalesListScreen() {
  const { t } = useTranslation();
  const router = useRouter();
  const insets = useSafeAreaInsets();
  const { shop } = useSession();
  const timeZone = shop?.timezone ?? Intl.DateTimeFormat().resolvedOptions().timeZone;
  const currency = shop?.currency ?? "UZS";

  const initialRange = useMemo(() => presetRange("today", timeZone), [timeZone]);
  const [fromInput, setFromInput] = useState(initialRange.from);
  const [toInput, setToInput] = useState(initialRange.to);
  const debouncedFrom = useDebouncedValue(fromInput, RANGE_DEBOUNCE_MS);
  const debouncedTo = useDebouncedValue(toInput, RANGE_DEBOUNCE_MS);
  const rangeValid = isValidRangeInput(debouncedFrom, debouncedTo);

  function applyPreset(preset: DateRangePreset) {
    const range = presetRange(preset, timeZone);
    setFromInput(range.from);
    setToInput(range.to);
  }

  const {
    data,
    isPending,
    isError,
    isRefetching,
    refetch,
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
  } = useSales(
    rangeValid
      ? { from: debouncedFrom.trim(), to: debouncedTo.trim() }
      : { from: initialRange.from, to: initialRange.to },
  );

  const sales = useMemo(() => data?.pages.flatMap((page) => page.items) ?? [], [data]);

  return (
    <View className="flex-1 bg-background">
      <View className="gap-3 px-4 pt-4">
        <Text variant="small">{t("sales.filters.dateRangeLabel")}</Text>
        <View className="flex-row flex-wrap gap-2">
          {DATE_RANGE_PRESETS.map((preset) => {
            const range = presetRange(preset, timeZone);
            const selected = fromInput === range.from && toInput === range.to;
            return (
              <Pressable
                key={preset}
                accessibilityRole="button"
                accessibilityState={{ selected }}
                className={`h-10 items-center justify-center rounded-md border px-3 ${
                  selected ? "border-primary bg-primary" : "border-input bg-background"
                }`}
                onPress={() => applyPreset(preset)}
              >
                <Text className={selected ? "text-primary-foreground" : undefined}>
                  {t(`reports.presets.${preset}`)}
                </Text>
              </Pressable>
            );
          })}
        </View>
        <View className="flex-row gap-3">
          <View className="flex-1 gap-1.5">
            <Text variant="small">{t("mobile.sale.list.fields.from")}</Text>
            <Input
              value={fromInput}
              onChangeText={setFromInput}
              placeholder="YYYY-MM-DD"
              autoCorrect={false}
              keyboardType="numbers-and-punctuation"
            />
          </View>
          <View className="flex-1 gap-1.5">
            <Text variant="small">{t("mobile.sale.list.fields.to")}</Text>
            <Input
              value={toInput}
              onChangeText={setToInput}
              placeholder="YYYY-MM-DD"
              autoCorrect={false}
              keyboardType="numbers-and-punctuation"
            />
          </View>
        </View>
        {!rangeValid && (
          <Text variant="small" className="text-destructive">
            {t("mobile.sale.list.errors.invalidRange")}
          </Text>
        )}
      </View>

      {isPending ? (
        <View className="flex-1 items-center justify-center">
          <ActivityIndicator />
        </View>
      ) : isError ? (
        <View className="flex-1 items-center justify-center gap-2 p-6">
          <Text variant="muted">{t("errors.generic")}</Text>
          <Pressable accessibilityRole="button" onPress={() => refetch()}>
            <Text className="text-primary">{t("common.retry")}</Text>
          </Pressable>
        </View>
      ) : (
        <FlatList
          className="flex-1 px-4"
          contentContainerStyle={{ paddingTop: 12, paddingBottom: insets.bottom + 12, gap: 8 }}
          data={sales}
          keyExtractor={(sale) => sale.id}
          refreshing={isRefetching}
          onRefresh={refetch}
          onEndReachedThreshold={0.4}
          onEndReached={() => {
            if (hasNextPage && !isFetchingNextPage) {
              fetchNextPage();
            }
          }}
          ListEmptyComponent={
            <Text variant="muted" className="p-4 text-center">
              {t("mobile.sale.list.emptyRange")}
            </Text>
          }
          ListFooterComponent={isFetchingNextPage ? <ActivityIndicator className="py-4" /> : null}
          renderItem={({ item: sale }) => (
            <SaleRow
              sale={sale}
              timeZone={timeZone}
              currency={currency}
              onPress={() => router.push(`/sale/${sale.id}`)}
            />
          )}
        />
      )}
    </View>
  );
}
