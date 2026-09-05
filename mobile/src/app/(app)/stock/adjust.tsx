import { Redirect, useRouter } from "expo-router";
import { useState } from "react";
import { Controller, useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { ActivityIndicator, KeyboardAvoidingView, Platform, ScrollView, View } from "react-native";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Text } from "@/components/ui/text";
import type { Variant } from "@/features/catalog/api";
import { useLocations } from "@/features/catalog/hooks";
import { ADJUSTMENT_REASONS, normalizeQty } from "@/features/stock/adjustmentForm";
import { type AdjustmentReason, newIdempotencyKey, StockApiError } from "@/features/stock/api";
import { ChipGroup } from "@/features/stock/ChipGroup";
import { useCreateStockAdjustment } from "@/features/stock/hooks";
import { VariantPickerModal } from "@/features/stock/VariantPickerModal";
import { useSession } from "@/lib/session";

interface AdjustFormValues {
  locationId: string | null;
  reason: AdjustmentReason | null;
  qty: string;
  note: string;
}

/** Details shape of a `409 STOCK_INSUFFICIENT` error (`docs/05-API.md` §
 * Conventions): `{ variantId, locationId, available }`. */
interface StockInsufficientDetails {
  available?: string;
}

/**
 * Manual stock adjustment (deliverable 3, D-46, `stock.write` only): variant
 * via the shared `VariantPicker`, location and reason via `ChipGroup`, a
 * signed quantity and an optional note. One `Idempotency-Key` is generated
 * when the screen mounts and reused on every retry of the same submission —
 * regenerated only by navigating here again (`docs/05-API.md` §
 * Conventions; mirrors `admin/src/routes/app/StockActionsDrawer.tsx`'s
 * `StockAdjustmentDrawer`, one dialog session = one key).
 */
export default function AdjustStockScreen() {
  const { t } = useTranslation();
  const router = useRouter();
  const { can } = useSession();

  const [idempotencyKey] = useState(newIdempotencyKey);
  const [selectedVariant, setSelectedVariant] = useState<Variant | null>(null);
  const [availableQty, setAvailableQty] = useState<string | null>(null);
  const [insufficientAvailable, setInsufficientAvailable] = useState<string | null>(null);

  const { data: locations } = useLocations();
  const activeLocations = (locations ?? []).filter((location) => location.isActive);

  const {
    control,
    handleSubmit,
    watch,
    formState: { errors, isSubmitting },
  } = useForm<AdjustFormValues>({
    defaultValues: { locationId: null, reason: null, qty: "", note: "" },
  });
  const locationId = watch("locationId");

  const mutation = useCreateStockAdjustment();

  if (!can("stock.write")) {
    return <Redirect href="/stock" />;
  }

  function variantLabel(variant: Variant): string {
    const attrs = Object.entries(variant.attributes)
      .map(([key, val]) => `${key}: ${val}`)
      .join(", ");
    return [variant.sku, attrs].filter(Boolean).join(" — ") || variant.id.slice(0, 8);
  }

  async function onSubmit(values: AdjustFormValues) {
    if (!selectedVariant || !values.locationId || !values.reason) {
      return;
    }
    const qty = normalizeQty(values.qty);
    if (!qty) {
      return;
    }
    setInsufficientAvailable(null);
    try {
      await mutation.mutateAsync({
        body: {
          variantId: selectedVariant.id,
          locationId: values.locationId,
          qty,
          reason: values.reason,
          ...(values.note.trim() ? { note: values.note.trim() } : {}),
        },
        idempotencyKey,
      });
      router.back();
    } catch (error) {
      if (error instanceof StockApiError && error.code === "STOCK_INSUFFICIENT") {
        const { available } = error.details as StockInsufficientDetails;
        setInsufficientAvailable(available ?? "0");
      }
      // Every other error surfaces via `mutation.isError`/`mutation.error`
      // below — the screen stays open so a retry reuses `idempotencyKey`.
    }
  }

  return (
    <KeyboardAvoidingView
      behavior={Platform.OS === "ios" ? "padding" : undefined}
      className="flex-1 bg-background"
    >
      <ScrollView
        contentContainerStyle={{ padding: 16, gap: 16 }}
        keyboardShouldPersistTaps="handled"
      >
        <View className="gap-1.5">
          <Text variant="small">{t("stock.fields.variant")}</Text>
          <VariantPickerModal
            locationId={locationId ?? activeLocations[0]?.id ?? ""}
            triggerLabel={
              selectedVariant ? variantLabel(selectedVariant) : t("stock.picker.variant")
            }
            disabled={activeLocations.length === 0}
            onPick={(variant, qty) => {
              setSelectedVariant(variant);
              setAvailableQty(qty);
            }}
          />
          {!selectedVariant && (
            <Text variant="small" className="text-destructive">
              {t("stock.errors.variantRequired")}
            </Text>
          )}
          {availableQty != null && (
            <Text variant="muted">
              {t("mobile.catalog.picker.qtyAvailable", { qty: availableQty })}
            </Text>
          )}
        </View>

        <View className="gap-1.5">
          <Text variant="small">{t("stock.fields.location")}</Text>
          <Controller
            control={control}
            name="locationId"
            rules={{ required: t("stock.errors.locationRequired") }}
            render={({ field }) => (
              <ChipGroup
                accessibilityLabel={t("stock.fields.location")}
                options={activeLocations.map((location) => ({
                  value: location.id,
                  label: location.name,
                }))}
                value={field.value}
                onChange={field.onChange}
              />
            )}
          />
          {errors.locationId && (
            <Text variant="small" className="text-destructive">
              {errors.locationId.message}
            </Text>
          )}
        </View>

        <View className="gap-1.5">
          <Text variant="small">{t("stock.fields.reason")}</Text>
          <Controller
            control={control}
            name="reason"
            rules={{ required: t("stock.errors.reasonRequired") }}
            render={({ field }) => (
              <ChipGroup
                accessibilityLabel={t("stock.fields.reason")}
                options={ADJUSTMENT_REASONS.map((reason) => ({
                  value: reason,
                  label: t(`stock.adjustmentReasons.${reason}`),
                }))}
                value={field.value}
                onChange={field.onChange}
              />
            )}
          />
          {errors.reason && (
            <Text variant="small" className="text-destructive">
              {errors.reason.message}
            </Text>
          )}
        </View>

        <View className="gap-1.5">
          <Text variant="small">{t("stock.fields.adjustmentQty")}</Text>
          <Text variant="muted">{t("stock.fields.adjustmentQtyHint")}</Text>
          <Controller
            control={control}
            name="qty"
            rules={{
              validate: (value) => normalizeQty(value) != null || t("stock.errors.qtyRequired"),
            }}
            render={({ field }) => (
              <Input
                value={field.value}
                onChangeText={field.onChange}
                onBlur={field.onBlur}
                keyboardType="numbers-and-punctuation"
                placeholder="0"
              />
            )}
          />
          {errors.qty && (
            <Text variant="small" className="text-destructive">
              {errors.qty.message}
            </Text>
          )}
          {insufficientAvailable != null && (
            <Text variant="small" className="text-destructive">
              {t("stock.errors.insufficient", { available: insufficientAvailable })}
            </Text>
          )}
        </View>

        <View className="gap-1.5">
          <Text variant="small">{t("stock.fields.note")}</Text>
          <Controller
            control={control}
            name="note"
            render={({ field }) => (
              <Input
                value={field.value}
                onChangeText={field.onChange}
                onBlur={field.onBlur}
                multiline
              />
            )}
          />
        </View>

        {mutation.isError &&
          !(
            mutation.error instanceof StockApiError && mutation.error.code === "STOCK_INSUFFICIENT"
          ) && (
            <Text variant="small" className="text-destructive">
              {t("errors.generic")}
            </Text>
          )}

        <Button
          disabled={!selectedVariant || isSubmitting || mutation.isPending}
          onPress={handleSubmit(onSubmit)}
        >
          {mutation.isPending ? <ActivityIndicator /> : <Text>{t("common.save")}</Text>}
        </Button>
      </ScrollView>
    </KeyboardAvoidingView>
  );
}
