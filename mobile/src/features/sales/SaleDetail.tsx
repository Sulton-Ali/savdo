import { useLocalSearchParams, useRouter } from "expo-router";
import { useTranslation } from "react-i18next";
import { ActivityIndicator, Pressable, ScrollView, View } from "react-native";

import { Text } from "@/components/ui/text";
import { formatQty } from "@/features/catalog/qty";
import { useSale } from "@/features/sales/hooks";
import { formatMoney } from "@/lib/money";
import { useSession } from "@/lib/session";

import type { SaleItem } from "./api";

function ItemRow({ item, currency }: { item: SaleItem; currency: string }) {
  const { t } = useTranslation();
  return (
    <View className="gap-1 rounded-md border border-border p-3">
      <Text numberOfLines={2}>
        {[item.productName, item.variantLabel].filter(Boolean).join(" — ")}
      </Text>
      <View className="flex-row items-center justify-between">
        <Text variant="muted">
          {formatQty(item.qty)} × {formatMoney(item.unitPrice, currency)}
        </Text>
        <Text variant="large">{formatMoney(item.lineTotal, currency)}</Text>
      </View>
      {item.returnedQty && !/^0(\.0+)?$/.test(item.returnedQty.trim()) ? (
        <Text variant="small" className="text-destructive">
          {t("sales.detail.columns.returnedQty")}: {formatQty(item.returnedQty)}
        </Text>
      ) : null}
    </View>
  );
}

/**
 * Read-only sale detail (T4 deliverable 3), `GET /sales/{id}` — any
 * `cashier+` may view any sale (D-63); `unitCost` is present on `items`
 * only for a caller with `cost.read` (manager+) and is never read here
 * regardless (ADR-010, hard rule 5, this task's own instruction 6). No
 * void/return action — those stay web-only for Phase 5 (task scope).
 *
 * Shared by two routes (T12 follow-up): `(tabs)/sale/[id].tsx` (`/sale/{id}`,
 * the Quick sale tab's own stack — reached from the post-Pay confirmation,
 * a customer's purchase history, or this same screen's own "view original"
 * link for a return) and `(tabs)/sales/[id].tsx` (`/sales/{id}`, the Sales
 * list tab's own stack, so its native back button and Android back return
 * to the list rather than unwinding across tabs). Both route files are a
 * one-line re-export of this component — no duplication, and each keeps
 * its own `id` param from `useLocalSearchParams` here.
 */
export default function SaleDetailScreen() {
  const { t } = useTranslation();
  const router = useRouter();
  const { id } = useLocalSearchParams<{ id: string }>();
  const { shop } = useSession();
  const timeZone = shop?.timezone ?? Intl.DateTimeFormat().resolvedOptions().timeZone;
  const currency = shop?.currency ?? "UZS";

  const saleQuery = useSale(id);

  if (saleQuery.isPending) {
    return (
      <View className="flex-1 items-center justify-center bg-background">
        <ActivityIndicator />
      </View>
    );
  }

  if (saleQuery.isError || !saleQuery.data) {
    return (
      <View className="flex-1 items-center justify-center gap-2 bg-background p-6">
        <Text variant="muted">{t("sales.detail.notFound")}</Text>
        <Pressable accessibilityRole="button" onPress={() => router.back()}>
          <Text className="text-primary">{t("common.back")}</Text>
        </Pressable>
      </View>
    );
  }

  const sale = saleQuery.data;
  const completedAt = new Intl.DateTimeFormat(undefined, {
    timeZone,
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(sale.completedAt));

  return (
    <ScrollView className="flex-1 bg-background" contentContainerStyle={{ padding: 16, gap: 16 }}>
      <View className="gap-1">
        <Text variant="h3">
          {t("sales.columns.number")} {sale.number}
        </Text>
        <Text variant="muted">{completedAt}</Text>
        <Text variant="muted">
          {t(`sales.kind.${sale.kind}`)} · {t(`sales.status.${sale.status}`)}
        </Text>
      </View>

      <View className="gap-1 rounded-md border border-border p-3">
        <View className="flex-row justify-between">
          <Text variant="muted">{t("sales.detail.fields.location")}</Text>
          <Text>{sale.locationName}</Text>
        </View>
        <View className="flex-row justify-between">
          <Text variant="muted">{t("sales.detail.fields.cashier")}</Text>
          <Text>{sale.cashierName}</Text>
        </View>
        <View className="flex-row justify-between">
          <Text variant="muted">{t("sales.detail.fields.customer")}</Text>
          <Text>{sale.customerName ?? t("mobile.sale.customer.none")}</Text>
        </View>
        <View className="flex-row justify-between">
          <Text variant="muted">{t("sales.detail.fields.payment")}</Text>
          <Text>{t(`sales.paymentMethod.${sale.payment.method}`)}</Text>
        </View>
        {sale.note ? (
          <View className="flex-row justify-between gap-2">
            <Text variant="muted">{t("sales.detail.fields.note")}</Text>
            <Text className="flex-1 text-right">{sale.note}</Text>
          </View>
        ) : null}
        {sale.originalSaleId ? (
          <Pressable
            accessibilityRole="button"
            onPress={() => router.push(`/sale/${sale.originalSaleId}`)}
          >
            <Text className="text-primary">{t("sales.detail.viewOriginal")}</Text>
          </Pressable>
        ) : null}
        {sale.status === "voided" ? (
          <View className="flex-row justify-between">
            <Text variant="muted">{t("sales.detail.fields.voidReason")}</Text>
            <Text>{sale.voidReason ?? "—"}</Text>
          </View>
        ) : null}
        {sale.hasReturns ? (
          <Text variant="small" className="text-destructive">
            {t("sales.detail.fields.hasReturns")}: {t("sales.detail.yes")}
          </Text>
        ) : null}
      </View>

      <View className="gap-2">
        <Text variant="large">{t("sales.detail.items")}</Text>
        {sale.items.map((item) => (
          <ItemRow key={item.id} item={item} currency={currency} />
        ))}
      </View>

      <View className="gap-1 rounded-md border border-border p-3">
        <View className="flex-row justify-between">
          <Text variant="muted">{t("sales.detail.totals.subtotal")}</Text>
          <Text>{formatMoney(sale.subtotal, currency)}</Text>
        </View>
        {sale.discountAmount && !/^0(\.0+)?$/.test(sale.discountAmount.trim()) ? (
          <View className="flex-row justify-between">
            <Text variant="muted">{t("sales.detail.totals.discount")}</Text>
            <Text>-{formatMoney(sale.discountAmount, currency)}</Text>
          </View>
        ) : null}
        <View className="flex-row justify-between">
          <Text variant="large">{t("sales.detail.totals.total")}</Text>
          <Text variant="large">{formatMoney(sale.total, currency)}</Text>
        </View>
      </View>
    </ScrollView>
  );
}
