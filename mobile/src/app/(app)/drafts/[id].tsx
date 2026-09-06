import { useQueryClient } from "@tanstack/react-query";
import { useLocalSearchParams, useRouter } from "expo-router";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { ActivityIndicator, Alert, Modal, Pressable, ScrollView, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";

import { Button } from "@/components/ui/button";
import { Text } from "@/components/ui/text";
import { useLocations } from "@/features/catalog/hooks";
import { formatQty } from "@/features/catalog/qty";
import type { PaymentMethod } from "@/features/sales/api";
import { SalesApiError } from "@/features/sales/api";
import { generateIdempotencyKey, idempotencyOutcome } from "@/features/sales/cart";
import { canManageDraft, parseUnavailableLineIndexes } from "@/features/sales/drafts";
import { useCompleteSaleDraft, useDeleteSaleDraft, useDraft } from "@/features/sales/hooks";
import { formatMoney } from "@/lib/money";
import { draftsKeys } from "@/lib/queryKeys";
import { useSession } from "@/lib/session";

const PAYMENT_METHODS: PaymentMethod[] = ["cash", "card", "transfer"];

/** `422 VALIDATION_FAILED details.fields` (`docs/05-API.md`'s complete
 * row) — mirrors `sale/index.tsx`'s `StockInsufficientDetails`. */
interface ValidationFailedDetails {
  fields?: Record<string, string>;
}

interface StockInsufficientDetails {
  variantId?: string;
  available?: string;
}

function PaymentMethodModal({
  visible,
  onClose,
  onPick,
}: {
  visible: boolean;
  onClose: () => void;
  onPick: (method: PaymentMethod) => void;
}) {
  const { t } = useTranslation();
  return (
    <Modal visible={visible} animationType="slide" transparent onRequestClose={onClose}>
      <Pressable className="flex-1 justify-end bg-black/40" onPress={onClose}>
        <Pressable
          onPress={(event) => event.stopPropagation()}
          className="gap-2 rounded-t-xl bg-card p-4"
        >
          <Text variant="h4" className="mb-2">
            {t("sales.fields.paymentMethod")}
          </Text>
          {PAYMENT_METHODS.map((method) => (
            <Pressable
              key={method}
              accessibilityRole="button"
              className="min-h-12 justify-center border-border border-b px-2 py-3 active:bg-accent"
              onPress={() => onPick(method)}
            >
              <Text>{t(`sales.paymentMethod.${method}`)}</Text>
            </Pressable>
          ))}
        </Pressable>
      </Pressable>
    </Modal>
  );
}

/**
 * Draft detail (T14 deliverable 3, D-87..D-90, D-96): read-only header +
 * lines + totals from `GET /sales/drafts/{id}`, plus three actions — Pay
 * (any staff who can create a sale, D-96), Edit (creator or manager+,
 * D-89, loads the draft into the quick-sale cart via
 * `/sale?draftId=`), Delete (creator or manager+, D-89). Prices/totals are
 * the server's own read-time computation (D-87) — never recomputed here.
 */
export default function DraftDetailScreen() {
  const { t } = useTranslation();
  const router = useRouter();
  const queryClient = useQueryClient();
  const insets = useSafeAreaInsets();
  const { id } = useLocalSearchParams<{ id: string }>();
  const { shop, me, can } = useSession();
  const currency = shop?.currency ?? "UZS";

  const draftQuery = useDraft(id);
  const draft = draftQuery.data;
  const { data: locations } = useLocations();
  const location = useMemo(
    () => (locations ?? []).find((l) => l.id === draft?.locationId),
    [locations, draft?.locationId],
  );
  const [paymentModalOpen, setPaymentModalOpen] = useState(false);
  const [completeKey, setCompleteKey] = useState(generateIdempotencyKey);
  const [generalError, setGeneralError] = useState<string | null>(null);
  const [possiblyRecorded, setPossiblyRecorded] = useState(false);
  const [unavailableIndexes, setUnavailableIndexes] = useState<number[]>([]);

  const completeDraft = useCompleteSaleDraft();
  const deleteDraft = useDeleteSaleDraft();

  if (draftQuery.isPending) {
    return (
      <View className="flex-1 items-center justify-center bg-background">
        <ActivityIndicator />
      </View>
    );
  }

  if (draftQuery.isError || !draft) {
    const notFound =
      draftQuery.error instanceof SalesApiError && draftQuery.error.code === "NOT_FOUND";
    return (
      <View className="flex-1 items-center justify-center gap-2 bg-background p-6">
        <Text variant="muted">
          {notFound ? t("mobile.drafts.errors.notFound") : t("errors.generic")}
        </Text>
        <Pressable accessibilityRole="button" onPress={() => router.back()}>
          <Text className="text-primary">{t("common.back")}</Text>
        </Pressable>
      </View>
    );
  }

  const canManage = canManageDraft(draft.createdBy, me?.user.id, can("sales.void"));
  const canPay = can("sales.create");

  // A `NOT_FOUND` here means someone else completed or deleted this exact
  // draft in the meantime — unlike every successful mutation on this
  // screen (`useCompleteSaleDraft`/`useDeleteSaleDraft` already invalidate
  // `draftsKeys.all` themselves), a *failed* one never runs that
  // invalidation, so the drafts list this screen came from would
  // otherwise still show the now-gone row until a manual pull-to-refresh
  // (found live during this task's own device smoke).
  function goBackAndRefresh() {
    queryClient.invalidateQueries({ queryKey: draftsKeys.all });
    router.back();
  }

  function handleChoosePayment(method: PaymentMethod) {
    setPaymentModalOpen(false);
    setGeneralError(null);
    setPossiblyRecorded(false);
    setUnavailableIndexes([]);
    completeDraft.mutate(
      { id, body: { paymentMethod: method }, idempotencyKey: completeKey },
      {
        onSuccess: (sale) => {
          // `push`, not `replace` — this screen is a Drawer-level sibling
          // of the `(tabs)` group (like `customers/[id].tsx`, whose own
          // doc comment on `SaleDetail.tsx` calls out this exact
          // cross-navigator jump as the established, working pattern).
          // `replace` from here was found live during this task's own
          // device smoke to collapse the Quick sale tab's own stack down
          // to just this one screen — with no `index` left beneath it,
          // the tab got permanently stuck showing this receipt, with no
          // way back to a fresh quick-sale form short of a full app
          // restart.
          router.push(`/sale/${sale.id}`);
        },
        onError: (error) => {
          if (error instanceof SalesApiError) {
            if (error.code === "VALIDATION_FAILED") {
              const fields = (error.details as ValidationFailedDetails | undefined)?.fields;
              const indexes = parseUnavailableLineIndexes(fields);
              if (indexes.length > 0) {
                setUnavailableIndexes(indexes);
                const firstName = draft?.items[indexes[0] as number]?.productName ?? "";
                setGeneralError(t("mobile.drafts.errors.lineUnavailable", { name: firstName }));
                return;
              }
            }
            if (error.code === "STOCK_INSUFFICIENT") {
              const details = error.details as StockInsufficientDetails | undefined;
              setGeneralError(
                t("sales.errors.stockInsufficient", { available: details?.available ?? "0" }),
              );
              return;
            }
            if (error.code === "DISCOUNT_EXCEEDS_SUBTOTAL") {
              setGeneralError(t("sales.errors.discountExceedsSubtotal"));
              return;
            }
            if (error.code === "FORBIDDEN") {
              setGeneralError(t("errors.forbidden"));
              return;
            }
            if (error.code === "NOT_FOUND") {
              setGeneralError(t("mobile.drafts.errors.notFound"));
              goBackAndRefresh();
              return;
            }
          }
          const code = error instanceof SalesApiError ? error.code : undefined;
          if (idempotencyOutcome(code) === "rekey") {
            setCompleteKey(generateIdempotencyKey());
            setGeneralError(t("sales.errors.idempotencyKeyReused"));
            setPossiblyRecorded(true);
            return;
          }
          if (!(error instanceof SalesApiError)) {
            setGeneralError(t("mobile.sale.errors.networkUnknown"));
            setPossiblyRecorded(true);
            return;
          }
          setGeneralError(t("errors.generic"));
        },
      },
    );
  }

  function handleEdit() {
    router.push({ pathname: "/sale", params: { draftId: draft?.id ?? "" } });
  }

  function handleDelete() {
    Alert.alert(t("mobile.drafts.deleteConfirm.title"), undefined, [
      { text: t("common.cancel"), style: "cancel" },
      {
        text: t("mobile.drafts.actions.delete"),
        style: "destructive",
        onPress: () => {
          deleteDraft.mutate(id, {
            onSuccess: () => router.back(),
            onError: (error) => {
              if (error instanceof SalesApiError && error.code === "NOT_FOUND") {
                setGeneralError(t("mobile.drafts.errors.notFound"));
                goBackAndRefresh();
                return;
              }
              if (error instanceof SalesApiError && error.code === "FORBIDDEN") {
                setGeneralError(t("errors.forbidden"));
                return;
              }
              setGeneralError(t("errors.generic"));
            },
          });
        },
      },
    ]);
  }

  const hasDiscount = draft.discountAmount && !/^0(\.0+)?$/.test(draft.discountAmount.trim());

  return (
    <View className="flex-1 bg-background">
      <ScrollView
        className="flex-1"
        contentContainerStyle={{ padding: 16, paddingBottom: insets.bottom + 16, gap: 16 }}
      >
        {generalError ? (
          <View className="gap-1 rounded-md bg-destructive/10 p-3">
            <Text className="text-destructive">{generalError}</Text>
            {possiblyRecorded ? (
              <Pressable accessibilityRole="button" onPress={() => router.push("/sales")}>
                <Text className="text-destructive underline">{t("mobile.sale.list.link")}</Text>
              </Pressable>
            ) : null}
          </View>
        ) : null}

        <View className="gap-1 rounded-md border border-border p-3">
          <View className="flex-row justify-between">
            <Text variant="muted">{t("sales.detail.fields.location")}</Text>
            <Text>{location?.name ?? "—"}</Text>
          </View>
          <View className="flex-row justify-between">
            <Text variant="muted">{t("sales.detail.fields.customer")}</Text>
            <Text>{draft.customerName ?? "—"}</Text>
          </View>
          {draft.note ? (
            <View className="flex-row justify-between gap-2">
              <Text variant="muted">{t("sales.fields.note")}</Text>
              <Text className="flex-1 text-right">{draft.note}</Text>
            </View>
          ) : null}
          {draft.discount ? (
            <View className="flex-row justify-between">
              <Text variant="muted">{t("sales.fields.discountType")}</Text>
              <Text>
                {t(`sales.discountType.${draft.discount.type}`)} · {draft.discount.value}
                {draft.discount.type === "percent" ? "%" : ""}
              </Text>
            </View>
          ) : null}
        </View>

        <View className="gap-2">
          <Text variant="large">{t("sales.detail.items")}</Text>
          {draft.items.map((item, index) => {
            const flagged = unavailableIndexes.includes(index);
            return (
              <View key={item.variantId} className="gap-1 rounded-md border border-border p-3">
                <View className="flex-row items-start justify-between gap-2">
                  <Text className="flex-1" numberOfLines={2}>
                    {[item.productName, item.variantLabel].filter(Boolean).join(" — ")}
                  </Text>
                  {!item.available ? (
                    <Text variant="small" className="text-destructive">
                      {t("mobile.drafts.detail.unavailable")}
                    </Text>
                  ) : null}
                </View>
                <View className="flex-row items-center justify-between">
                  <Text variant="muted">
                    {formatQty(item.qty)} × {formatMoney(item.unitPrice, currency)}
                  </Text>
                  <Text variant="large">{formatMoney(item.lineTotal, currency)}</Text>
                </View>
                {flagged ? (
                  <Text variant="small" className="text-destructive">
                    {t("mobile.drafts.errors.lineUnavailable", { name: item.productName })}
                  </Text>
                ) : null}
              </View>
            );
          })}
        </View>

        <View className="gap-1 rounded-md border border-border p-3">
          <Text variant="small" className="text-muted-foreground">
            {t("mobile.sale.estimate")}
          </Text>
          <View className="flex-row justify-between">
            <Text variant="muted">{t("sales.detail.totals.subtotal")}</Text>
            <Text>{formatMoney(draft.subtotal, currency)}</Text>
          </View>
          {hasDiscount ? (
            <View className="flex-row justify-between">
              <Text variant="muted">{t("sales.detail.totals.discount")}</Text>
              <Text>-{formatMoney(draft.discountAmount, currency)}</Text>
            </View>
          ) : null}
          <View className="flex-row justify-between">
            <Text variant="large">{t("sales.detail.totals.total")}</Text>
            <Text variant="large">{formatMoney(draft.estimatedTotal, currency)}</Text>
          </View>
        </View>
      </ScrollView>

      <View
        className="gap-2 border-border border-t bg-background px-4 pt-4"
        style={{ paddingBottom: Math.max(insets.bottom, 16) }}
      >
        {canPay ? (
          <Button
            size="lg"
            disabled={completeDraft.isPending}
            onPress={() => setPaymentModalOpen(true)}
          >
            {completeDraft.isPending ? (
              <>
                <ActivityIndicator />
                <Text>{t("mobile.sale.paying")}</Text>
              </>
            ) : (
              <Text>{t("mobile.sale.pay")}</Text>
            )}
          </Button>
        ) : null}
        {canManage ? (
          <View className="flex-row gap-2">
            <Button variant="outline" className="flex-1" onPress={handleEdit}>
              <Text>{t("mobile.drafts.actions.edit")}</Text>
            </Button>
            <Button
              variant="destructive"
              className="flex-1"
              disabled={deleteDraft.isPending}
              onPress={handleDelete}
            >
              <Text>{t("mobile.drafts.actions.delete")}</Text>
            </Button>
          </View>
        ) : null}
      </View>

      <PaymentMethodModal
        visible={paymentModalOpen}
        onClose={() => setPaymentModalOpen(false)}
        onPick={handleChoosePayment}
      />
    </View>
  );
}
