import { Redirect, useLocalSearchParams } from "expo-router";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { ActivityIndicator, ScrollView, View } from "react-native";

import { useLocations } from "@/features/catalog/hooks";
import { newIdempotencyKey, PurchasesApiError } from "@/features/purchases/api";
import { usePurchase, useReceivePurchase, useSuppliers } from "@/features/purchases/hooks";

import { Button } from "@/components/ui/button";
import { Text } from "@/components/ui/text";
import { formatMoney } from "@/lib/money";
import { useSession } from "@/lib/session";

/** Details shape of a `409 STOCK_INSUFFICIENT` error. Not expected on a
 * draft purchase's own receive (nothing has moved its stock yet), kept only
 * as a defensive, informative fallback. */
interface StockInsufficientDetails {
  available?: string;
}

/**
 * Purchase detail (deliverable 3, `stock.write` only): shows a draft or
 * already-received/cancelled purchase's items, and offers "Receive" for a
 * `draft` one — including a draft created on the admin web (D-80). The
 * `Idempotency-Key` is generated once when this screen mounts and reused on
 * every retry of "Receive" from this same screen instance (`docs/05-API.md`
 * § Conventions; mirrors `admin/src/routes/app/PurchaseFormPage.tsx`'s
 * receive dialog). A `409 PURCHASE_ALREADY_RECEIVED` (e.g. someone else
 * received the same draft first) is handled gracefully by
 * `useReceivePurchase` itself, which then shows the now-received purchase
 * instead of an error.
 */
export default function PurchaseDetailScreen() {
  const { t } = useTranslation();
  const { can, shop } = useSession();
  const { id } = useLocalSearchParams<{ id: string }>();
  const currency = shop?.currency ?? "UZS";

  const [idempotencyKey] = useState(newIdempotencyKey);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);

  const purchaseQuery = usePurchase(id);
  const { data: suppliers } = useSuppliers();
  const { data: locations } = useLocations();
  const receiveMutation = useReceivePurchase();

  const supplierName = useMemo(
    () => suppliers?.find((supplier) => supplier.id === purchaseQuery.data?.supplierId)?.name,
    [suppliers, purchaseQuery.data],
  );
  const locationName = useMemo(
    () => locations?.find((location) => location.id === purchaseQuery.data?.locationId)?.name,
    [locations, purchaseQuery.data],
  );

  if (!can("stock.write")) {
    return <Redirect href="/stock" />;
  }

  if (purchaseQuery.isPending) {
    return (
      <View className="flex-1 items-center justify-center bg-background">
        <ActivityIndicator />
      </View>
    );
  }

  if (purchaseQuery.isError || !purchaseQuery.data) {
    return (
      <View className="flex-1 items-center justify-center bg-background p-6">
        <Text variant="muted">{t("errors.generic")}</Text>
      </View>
    );
  }

  const purchase = receiveMutation.data ?? purchaseQuery.data;

  async function handleReceive() {
    setErrorMessage(null);
    try {
      await receiveMutation.mutateAsync({ id: id as string, idempotencyKey });
    } catch (error) {
      if (error instanceof PurchasesApiError && error.code === "STOCK_INSUFFICIENT") {
        const { available } = error.details as StockInsufficientDetails;
        setErrorMessage(t("stock.errors.insufficient", { available: available ?? "0" }));
        return;
      }
      if (error instanceof PurchasesApiError && error.code === "PURCHASE_ALREADY_CANCELLED") {
        setErrorMessage(t("purchases.status.cancelled"));
        return;
      }
      setErrorMessage(t("errors.generic"));
    }
  }

  return (
    <ScrollView className="flex-1 bg-background" contentContainerStyle={{ padding: 16, gap: 16 }}>
      <View className="gap-1">
        <Text variant="h4">{purchase.number}</Text>
        <Text variant="muted">{t(`purchases.status.${purchase.status}`)}</Text>
      </View>

      <View className="gap-1">
        <Text variant="small">{t("purchases.fields.supplier")}</Text>
        <Text>{supplierName ?? "—"}</Text>
      </View>
      <View className="gap-1">
        <Text variant="small">{t("purchases.fields.location")}</Text>
        <Text>{locationName ?? "—"}</Text>
      </View>
      {purchase.supplierInvoiceNo && (
        <View className="gap-1">
          <Text variant="small">{t("purchases.fields.supplierInvoiceNo")}</Text>
          <Text>{purchase.supplierInvoiceNo}</Text>
        </View>
      )}
      {purchase.note && (
        <View className="gap-1">
          <Text variant="small">{t("purchases.fields.note")}</Text>
          <Text>{purchase.note}</Text>
        </View>
      )}

      <View className="gap-2">
        <Text variant="small">{t("purchases.form.items.title")}</Text>
        {purchase.items.map((item) => (
          <View
            key={item.id}
            className="flex-row items-center justify-between rounded-md border border-border p-3"
          >
            <View className="flex-1 pr-2">
              <Text numberOfLines={1}>{item.productName}</Text>
              <Text variant="muted">{item.variantLabel}</Text>
            </View>
            <Text variant="muted">
              {item.qty} × {formatMoney(item.unitCost, currency)}
            </Text>
          </View>
        ))}
        <Text className="text-right">
          {t("purchases.receive.total", { total: formatMoney(purchase.totalCost, currency) })}
        </Text>
      </View>

      {errorMessage && (
        <Text variant="small" className="text-destructive">
          {errorMessage}
        </Text>
      )}

      {purchase.status === "draft" && (
        <Button disabled={receiveMutation.isPending} onPress={handleReceive}>
          {receiveMutation.isPending ? (
            <ActivityIndicator />
          ) : (
            <Text>{t("purchases.receive.action")}</Text>
          )}
        </Button>
      )}
    </ScrollView>
  );
}
