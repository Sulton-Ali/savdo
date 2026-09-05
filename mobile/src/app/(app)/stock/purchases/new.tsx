import { Redirect, useRouter } from "expo-router";
import { useReducer, useState } from "react";
import { useTranslation } from "react-i18next";
import { ActivityIndicator, KeyboardAvoidingView, Platform, ScrollView, View } from "react-native";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Text } from "@/components/ui/text";
import type { Variant } from "@/features/catalog/api";
import { useLocations } from "@/features/catalog/hooks";
import { newIdempotencyKey, type Purchase, PurchasesApiError } from "@/features/purchases/api";
import {
  buildCreateBody,
  createInitialDraft,
  isPurchaseDraftValid,
  normalizeQty,
  normalizeUnitCost,
  purchaseDraftReducer,
} from "@/features/purchases/draft";
import { useCreatePurchase, useReceivePurchase, useSuppliers } from "@/features/purchases/hooks";
import { ChipGroup } from "@/features/stock/ChipGroup";
import { VariantPickerModal } from "@/features/stock/VariantPickerModal";
import { formatMoney } from "@/lib/money";
import { useSession } from "@/lib/session";

/** Details shape of a `409 STOCK_INSUFFICIENT` error (`docs/05-API.md` §
 * Conventions). Not expected here in practice (receiving a purchase this
 * screen just created can't go below zero), kept only as a defensive,
 * informative fallback instead of the generic error text. */
interface StockInsufficientDetails {
  available?: string;
  variantId?: string;
}

/**
 * "New purchase" (deliverable 3, D-80, `stock.write` only): supplier,
 * location, lines added via the shared `VariantPicker` with qty and unit
 * cost, then "Save and receive" — `createPurchase` followed immediately by
 * `receivePurchase` in the same flow. State lives in the pure
 * `purchaseDraftReducer` (`features/purchases/draft.ts`); this screen only
 * wires it to inputs and the two mutations.
 */
export default function NewPurchaseScreen() {
  const { t } = useTranslation();
  const router = useRouter();
  const { can } = useSession();

  const [draft, dispatch] = useReducer(purchaseDraftReducer, undefined, () =>
    createInitialDraft(newIdempotencyKey()),
  );
  const [pendingVariant, setPendingVariant] = useState<Variant | null>(null);
  const [pendingQty, setPendingQty] = useState("");
  const [pendingUnitCost, setPendingUnitCost] = useState("");
  const [submitError, setSubmitError] = useState<string | null>(null);
  // `createPurchase`'s response already carries server-resolved
  // `productName`/`variantLabel`/`sku` per item (`PurchaseItem`, unlike the
  // bare `Variant` the picker returns pre-creation) — kept so the read-only
  // view after "Save and receive" shows real product names instead of the
  // picker's variant-only label.
  const [createdPurchase, setCreatedPurchase] = useState<Purchase | null>(null);

  const { data: suppliers } = useSuppliers();
  const { data: locations } = useLocations();
  const activeLocations = (locations ?? []).filter((location) => location.isActive);

  const createPurchase = useCreatePurchase();
  const receivePurchase = useReceivePurchase();
  const submitting = createPurchase.isPending || receivePurchase.isPending;

  if (!can("stock.write")) {
    return <Redirect href="/stock" />;
  }

  const editable = draft.createdPurchaseId == null;
  const received = receivePurchase.isSuccess;

  function variantLabel(variant: Variant): string {
    const attrs = Object.entries(variant.attributes)
      .map(([key, val]) => `${key}: ${val}`)
      .join(", ");
    return [variant.sku, attrs].filter(Boolean).join(" — ") || variant.id.slice(0, 8);
  }

  function handleAddLine() {
    if (!pendingVariant) {
      return;
    }
    const qty = normalizeQty(pendingQty);
    const unitCost = normalizeUnitCost(pendingUnitCost);
    if (!qty || !unitCost) {
      return;
    }
    const variant = pendingVariant;
    // The picker only returns the bare `Variant` (no product name — see
    // `VariantPicker`'s `onPick` signature, out of this task's file scope
    // to change), so `productName` is the variant's own label until
    // `createdPurchase` below replaces this whole list with the server's
    // resolved names.
    dispatch({
      type: "addLine",
      line: {
        variantId: variant.id,
        productName: variantLabel(variant),
        variantLabel: variantLabel(variant),
        sku: variant.sku,
        qty,
        unitCost,
      },
    });
    setPendingVariant(null);
    setPendingQty("");
    setPendingUnitCost("");
  }

  async function handleSaveAndReceive() {
    setSubmitError(null);
    let purchaseId = draft.createdPurchaseId;

    if (!purchaseId) {
      const body = buildCreateBody(draft);
      if (!body) {
        return;
      }
      try {
        const created = await createPurchase.mutateAsync(body);
        purchaseId = created.id;
        setCreatedPurchase(created);
        dispatch({ type: "purchaseCreated", id: created.id, number: created.number });
      } catch {
        setSubmitError(t("errors.generic"));
        return;
      }
    }

    try {
      await receivePurchase.mutateAsync({
        id: purchaseId,
        idempotencyKey: draft.receiveIdempotencyKey,
      });
    } catch (error) {
      if (error instanceof PurchasesApiError && error.code === "STOCK_INSUFFICIENT") {
        const { available, variantId } = error.details as StockInsufficientDetails;
        const line = draft.lines.find((candidate) => candidate.variantId === variantId);
        setSubmitError(
          t("purchases.errors.stockInsufficient", {
            variant: line?.productName ?? variantId?.slice(0, 8) ?? "—",
            available: available ?? "0",
          }),
        );
        return;
      }
      setSubmitError(t("errors.generic"));
    }
  }

  const currency = "UZS";

  return (
    <KeyboardAvoidingView
      behavior={Platform.OS === "ios" ? "padding" : undefined}
      className="flex-1 bg-background"
    >
      <ScrollView
        contentContainerStyle={{ padding: 16, gap: 16 }}
        keyboardShouldPersistTaps="handled"
      >
        {received && (
          <View className="rounded-md bg-primary/10 p-3">
            <Text className="text-primary">
              {t("purchases.receive.success")} — {draft.createdPurchaseNumber}
            </Text>
          </View>
        )}

        <View className="gap-1.5">
          <Text variant="small">{t("purchases.fields.supplier")}</Text>
          <ChipGroup
            accessibilityLabel={t("purchases.fields.supplier")}
            options={(suppliers ?? []).map((supplier) => ({
              value: supplier.id,
              label: supplier.name,
            }))}
            value={draft.supplierId}
            disabled={!editable}
            onChange={(supplierId) => dispatch({ type: "setSupplier", supplierId })}
          />
        </View>

        <View className="gap-1.5">
          <Text variant="small">{t("purchases.fields.location")}</Text>
          <ChipGroup
            accessibilityLabel={t("purchases.fields.location")}
            options={activeLocations.map((location) => ({
              value: location.id,
              label: location.name,
            }))}
            value={draft.locationId}
            disabled={!editable}
            onChange={(locationId) => dispatch({ type: "setLocation", locationId })}
          />
        </View>

        <View className="gap-1.5">
          <Text variant="small">{t("purchases.fields.supplierInvoiceNo")}</Text>
          <Input
            value={draft.supplierInvoiceNo}
            editable={editable}
            onChangeText={(value) => dispatch({ type: "setSupplierInvoiceNo", value })}
          />
        </View>

        <View className="gap-1.5">
          <Text variant="small">{t("purchases.fields.note")}</Text>
          <Input
            value={draft.note}
            editable={editable}
            multiline
            onChangeText={(value) => dispatch({ type: "setNote", value })}
          />
        </View>

        <View className="gap-2">
          <Text variant="small">{t("purchases.form.items.title")}</Text>
          {editable ? (
            draft.lines.length === 0 ? (
              <Text variant="muted">{t("purchases.form.items.empty")}</Text>
            ) : (
              draft.lines.map((line) => (
                <View
                  key={line.variantId}
                  className="flex-row items-center justify-between rounded-md border border-border p-3"
                >
                  <View className="flex-1 pr-2">
                    <Text numberOfLines={1}>{line.productName}</Text>
                    <Text variant="muted">
                      {line.qty} × {formatMoney(line.unitCost, currency)}
                    </Text>
                  </View>
                  <Button
                    variant="destructive"
                    size="sm"
                    onPress={() => dispatch({ type: "removeLine", variantId: line.variantId })}
                  >
                    <Text>{t("purchases.form.items.remove")}</Text>
                  </Button>
                </View>
              ))
            )
          ) : (
            // Once created, show the server's resolved item list
            // (`createdPurchase.items` — real `productName`/`variantLabel`)
            // instead of the client-composed draft lines.
            (createdPurchase?.items ?? []).map((item) => (
              <View
                key={item.id}
                className="flex-row items-center justify-between rounded-md border border-border p-3"
              >
                <View className="flex-1 pr-2">
                  <Text numberOfLines={1}>{item.productName}</Text>
                  <Text variant="muted">
                    {item.variantLabel} · {item.qty} × {formatMoney(item.unitCost, currency)}
                  </Text>
                </View>
              </View>
            ))
          )}

          {editable && (
            <View className="gap-2 rounded-md border border-border border-dashed p-3">
              <VariantPickerModal
                locationId={draft.locationId ?? activeLocations[0]?.id ?? ""}
                excludeVariantIds={draft.lines.map((line) => line.variantId)}
                disabled={activeLocations.length === 0}
                triggerLabel={
                  pendingVariant
                    ? variantLabel(pendingVariant)
                    : t("purchases.form.items.productPlaceholder")
                }
                onPick={setPendingVariant}
              />
              {pendingVariant && (
                <>
                  <Input
                    value={pendingQty}
                    onChangeText={setPendingQty}
                    placeholder={t("purchases.form.items.qty")}
                    keyboardType="numbers-and-punctuation"
                  />
                  <Input
                    value={pendingUnitCost}
                    onChangeText={setPendingUnitCost}
                    placeholder={t("purchases.form.items.unitCost")}
                    keyboardType="numbers-and-punctuation"
                  />
                  <Button
                    variant="secondary"
                    disabled={!normalizeQty(pendingQty) || !normalizeUnitCost(pendingUnitCost)}
                    onPress={handleAddLine}
                  >
                    <Text>{t("purchases.form.items.add")}</Text>
                  </Button>
                </>
              )}
            </View>
          )}
          {editable && draft.lines.length === 0 && (
            <Text variant="small" className="text-destructive">
              {t("purchases.form.items.required")}
            </Text>
          )}
        </View>

        {submitError && (
          <Text variant="small" className="text-destructive">
            {submitError}
          </Text>
        )}

        <Button
          disabled={submitting || received || (editable && !isPurchaseDraftValid(draft))}
          onPress={handleSaveAndReceive}
        >
          {submitting ? <ActivityIndicator /> : <Text>{t("mobile.purchases.saveAndReceive")}</Text>}
        </Button>

        {received && draft.createdPurchaseId && (
          <Button
            variant="outline"
            onPress={() => router.replace(`/stock/purchases/${draft.createdPurchaseId}`)}
          >
            <Text>{t("common.back")}</Text>
          </Button>
        )}
      </ScrollView>
    </KeyboardAvoidingView>
  );
}
