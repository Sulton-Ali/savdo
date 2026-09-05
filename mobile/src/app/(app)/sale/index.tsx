import { tokens } from "@savdo/ui-tokens";
import { useLocalSearchParams, useRouter } from "expo-router";
import { Minus, Plus, Trash2, UserPlus } from "lucide-react-native";
import { useEffect, useMemo, useReducer, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  ActivityIndicator,
  FlatList,
  KeyboardAvoidingView,
  Modal,
  Platform,
  Pressable,
  ScrollView,
  TextInput,
  View,
} from "react-native";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Text } from "@/components/ui/text";
import type { Location, Product, Variant } from "@/features/catalog/api";
import { useDebouncedValue, useLocations } from "@/features/catalog/hooks";
import { resolveEffectivePrice } from "@/features/catalog/pricing";
import { VariantPicker } from "@/features/catalog/VariantPicker";
import type { Customer } from "@/features/customers/api";
import { useCustomersSearch } from "@/features/customers/hooks";
import {
  type PaymentMethod,
  type Sale,
  type SaleCreate,
  SalesApiError,
} from "@/features/sales/api";
import {
  cartReducer,
  type DiscountKind,
  estimateCartTotals,
  idempotencyOutcome,
  initialCartState,
  isValidDiscountValue,
  isZeroDecimalString,
  multiplyMoneyByQty,
  qtyExceedsAvailable,
} from "@/features/sales/cart";
import { useCreateSale } from "@/features/sales/hooks";
import { persistLocationId, readStoredLocationId } from "@/features/sales/locationStorage";
import { formatMoney } from "@/lib/money";
import { useSession } from "@/lib/session";

const PAYMENT_METHODS: PaymentMethod[] = ["cash", "card", "transfer"];
const DISCOUNT_KINDS: Array<"none" | DiscountKind> = ["none", "percent", "fixed"];

/** `409 STOCK_INSUFFICIENT details.variantId/available` (`docs/05-API.md`
 * § Conventions). */
interface StockInsufficientDetails {
  variantId?: string;
  available?: string;
}

interface SelectedCustomer {
  id: string;
  fullName: string;
  phone: string | null;
}

/** Search-and-pick (or create) a customer to attach to the sale — a
 * simpler cousin of `features/catalog/VariantPicker.tsx`, one step, no
 * product/variant nesting, plus a "New customer" footer that hands off to
 * `customers/new.tsx` and back (`sale/index.tsx`'s own `attachCustomerId`
 * handling below has the full round-trip). */
function CustomerPickerModal({
  visible,
  onClose,
  onPick,
}: {
  visible: boolean;
  onClose: () => void;
  onPick: (customer: SelectedCustomer) => void;
}) {
  const { t } = useTranslation();
  const router = useRouter();
  const [rawQuery, setRawQuery] = useState("");
  const debouncedQuery = useDebouncedValue(rawQuery, 300);
  const { data, isFetching } = useCustomersSearch(debouncedQuery);
  const customers = useMemo(() => data?.pages.flatMap((page) => page.items) ?? [], [data]);

  // Resets the search query on every close path (Cancel, backdrop/hardware
  // back, picking a row, handing off to "New customer") so reopening the
  // sheet never shows a stale query from the last time it was used (T4
  // review nit).
  function handleClose() {
    setRawQuery("");
    onClose();
  }

  function handlePick(customer: Customer) {
    onPick({ id: customer.id, fullName: customer.fullName, phone: customer.phone });
    handleClose();
  }

  function handleNewCustomer() {
    handleClose();
    router.push({ pathname: "/customers/new", params: { returnTo: "sale" } });
  }

  return (
    <Modal visible={visible} animationType="slide" onRequestClose={handleClose}>
      <View className="flex-1 gap-3 bg-background p-4 pt-12">
        <View className="flex-row items-center justify-between">
          <Text variant="h4">{t("sales.fields.customer")}</Text>
          <Pressable
            accessibilityRole="button"
            onPress={handleClose}
            className="min-h-11 justify-center"
          >
            <Text className="text-primary">{t("common.cancel")}</Text>
          </Pressable>
        </View>
        <TextInput
          className="h-12 rounded-md border border-input bg-background px-3 text-base text-foreground"
          placeholder={t("sales.customerPlaceholder")}
          value={rawQuery}
          onChangeText={setRawQuery}
          autoCorrect={false}
          accessibilityLabel={t("sales.customerPlaceholder")}
        />
        <FlatList
          data={customers}
          keyExtractor={(customer) => customer.id}
          ListEmptyComponent={
            !isFetching ? (
              <Text variant="muted" className="p-4 text-center">
                {t("mobile.customers.list.empty")}
              </Text>
            ) : null
          }
          renderItem={({ item }) => (
            <Pressable
              accessibilityRole="button"
              className="min-h-12 justify-center border-border border-b px-2 py-3 active:bg-accent"
              onPress={() => handlePick(item)}
            >
              <Text numberOfLines={1}>{item.fullName}</Text>
              {item.phone ? (
                <Text variant="muted" numberOfLines={1}>
                  {item.phone}
                </Text>
              ) : null}
            </Pressable>
          )}
        />
        <Button variant="outline" onPress={handleNewCustomer}>
          <UserPlus size={18} color={tokens.color.primary} />
          <Text>{t("mobile.sale.customer.newCustomer")}</Text>
        </Button>
      </View>
    </Modal>
  );
}

function LocationPickerModal({
  visible,
  locations,
  onClose,
  onPick,
}: {
  visible: boolean;
  locations: Location[];
  onClose: () => void;
  onPick: (location: Location) => void;
}) {
  const { t } = useTranslation();
  return (
    <Modal visible={visible} animationType="slide" transparent onRequestClose={onClose}>
      <Pressable className="flex-1 justify-end bg-black/40" onPress={onClose}>
        <Pressable
          onPress={(event) => event.stopPropagation()}
          className="rounded-t-xl bg-card p-4"
        >
          <Text variant="h4" className="mb-2">
            {t("mobile.sale.location.title")}
          </Text>
          {locations.map((location) => (
            <Pressable
              key={location.id}
              accessibilityRole="button"
              className="min-h-12 justify-center border-border border-b px-2 py-3 active:bg-accent"
              onPress={() => {
                onPick(location);
                onClose();
              }}
            >
              <Text>{location.name}</Text>
            </Pressable>
          ))}
        </Pressable>
      </Pressable>
    </Modal>
  );
}

/**
 * One-handed quick sale (T4, `docs/03-ARCHITECTURE.md` § Quick sale flow,
 * D-52..D-57): pick a location (remembered, and re-validated against the
 * shop's actual locations on every launch — see the location-bootstrap
 * effect below, T4 review BLOCKER), search/pick variants with the shared
 * `VariantPicker` (T2, its `onPick` gained an additive `product` argument
 * in this review round), adjust quantities, optionally discount and
 * attach a customer, choose a payment method, and pay. A cart line's
 * price/product name and the cart's subtotal/discount/total are a
 * *preview* only, resolved from the catalogue the same way `VariantPicker`
 * itself does (`resolveEffectivePrice`) and clearly labelled as an
 * estimate (`mobile.sale.estimate`) — the confirmation after payment shows
 * only the server's own numbers, which is the only place a total is ever
 * authoritative (D-56, hard rule 8; `features/sales/cart.ts`'s own doc
 * comment has the full reasoning).
 */
export default function SaleScreen() {
  const { t } = useTranslation();
  const router = useRouter();
  const { shop } = useSession();
  const currency = shop?.currency ?? "UZS";
  const timeZone = shop?.timezone ?? Intl.DateTimeFormat().resolvedOptions().timeZone;
  const params = useLocalSearchParams<{
    attachCustomerId?: string;
    attachCustomerName?: string;
    attachCustomerPhone?: string;
  }>();

  const [cart, dispatch] = useReducer(cartReducer, undefined, initialCartState);
  const [lineErrors, setLineErrors] = useState<Record<string, string>>({});
  const [generalError, setGeneralError] = useState<string | null>(null);
  // `true` when the last `POST /sales` failed in a way that leaves the
  // outcome genuinely unknown — `409 IDEMPOTENCY_KEY_REUSED` (an earlier
  // attempt with this key already succeeded), or any error that isn't a
  // decoded server response at all (a network drop or a timeout never got
  // as far as a response either, so it's equally unknown whether the
  // request committed). Shown alongside `generalError` with a link to
  // today's sales list so the cashier can check before paying again (T4
  // review SHOULD-FIX 3 / nit 9). Note this is independent of whether the
  // key is rekeyed (`idempotencyOutcome`) — an undecoded error keeps the
  // key (T4 review CRITICAL) but is just as "possibly recorded" as the
  // reused-key case.
  const [possiblyRecorded, setPossiblyRecorded] = useState(false);
  const [completedSale, setCompletedSale] = useState<Sale | null>(null);

  const {
    data: locations,
    isPending: locationsPending,
    isError: locationsError,
    refetch: refetchLocations,
  } = useLocations();
  const activeLocations = useMemo(() => (locations ?? []).filter((l) => l.isActive), [locations]);
  const noActiveLocations = !locationsPending && !locationsError && activeLocations.length === 0;

  // The remembered location id is only ever a *hint*: it might name a
  // location that has since been deactivated, deleted, or — since
  // `locationStorage.ts` namespaces it by server origin — could only reach
  // this state at all from a stored value that predates that namespacing.
  // Every screen control below gates on `selectedLocation`, the resolved
  // `Location` object, never on a raw id (T4 review BLOCKER): a truthy but
  // unresolved id must never be treated as "a location is selected".
  const [selectedLocation, setSelectedLocation] = useState<Location | null>(null);
  const [storedLocationId, setStoredLocationId] = useState<string | null>(null);
  const [locationHydrated, setLocationHydrated] = useState(false);
  useEffect(() => {
    let cancelled = false;
    readStoredLocationId().then((id) => {
      if (!cancelled) {
        setStoredLocationId(id);
        setLocationHydrated(true);
      }
    });
    return () => {
      cancelled = true;
    };
  }, []);
  // Once the shop's locations load, accept the remembered id only if it
  // names a location that is actually active right now; otherwise fall
  // back to the shop's default location, else the first active one — a
  // cashier who has never picked one yet (or whose remembered one is no
  // longer valid) still gets a location pre-selected rather than a
  // mandatory extra tap every sale. A rejected/missing remembered id is
  // overwritten with the resolved fallback so this doesn't repeat next launch.
  useEffect(() => {
    if (selectedLocation != null || !locationHydrated || activeLocations.length === 0) {
      return;
    }
    const remembered = activeLocations.find((l) => l.id === storedLocationId);
    const resolved =
      remembered ?? activeLocations.find((l) => l.isDefault) ?? (activeLocations[0] as Location);
    setSelectedLocation(resolved);
    if (resolved.id !== storedLocationId) {
      void persistLocationId(resolved.id);
      // Resync so this state agrees with what was just written to
      // SecureStore (T4 review nit) — otherwise `storedLocationId` keeps
      // naming the rejected/missing id, and anything that re-reads it
      // later (or a future re-run of this same effect, should
      // `selectedLocation` ever reset) would see a stale value instead of
      // the fallback that's actually in effect now.
      setStoredLocationId(resolved.id);
    }
  }, [selectedLocation, locationHydrated, storedLocationId, activeLocations]);

  const [locationModalOpen, setLocationModalOpen] = useState(false);
  const [pickerOpen, setPickerOpen] = useState(false);
  const [customerModalOpen, setCustomerModalOpen] = useState(false);
  const [customer, setCustomer] = useState<SelectedCustomer | null>(null);
  const [paymentMethod, setPaymentMethod] = useState<PaymentMethod>("cash");
  const [discountKind, setDiscountKind] = useState<"none" | DiscountKind>("none");

  // A customer created from `customers/new.tsx` (reached via this screen's
  // "New customer" button) comes back as three route params rather than a
  // second `GET /customers/{id}` round trip — `new.tsx`'s own doc comment
  // has the full round-trip. Applied at most once per created customer (the
  // ref guards a re-render from re-applying the same params after the user
  // has since changed the attached customer again).
  const appliedAttachIdRef = useRef<string | null>(null);
  useEffect(() => {
    if (!params.attachCustomerId || params.attachCustomerId === appliedAttachIdRef.current) {
      return;
    }
    appliedAttachIdRef.current = params.attachCustomerId;
    setCustomer({
      id: params.attachCustomerId,
      fullName: params.attachCustomerName ?? "",
      phone: params.attachCustomerPhone || null,
    });
    router.setParams({
      attachCustomerId: undefined,
      attachCustomerName: undefined,
      attachCustomerPhone: undefined,
    });
  }, [params.attachCustomerId, params.attachCustomerName, params.attachCustomerPhone, router]);

  const discountValueInvalid =
    discountKind !== "none" &&
    cart.discount != null &&
    cart.discount.value.trim() !== "" &&
    !isValidDiscountValue(discountKind, cart.discount.value);

  // Decimal-safe preview only — never sent to the server, never shown as
  // "the total" (that word is reserved for the confirmation screen's own
  // `completedSale.total`, T4 review SHOULD-FIX 2). Recomputed on every
  // render from the lines' pick-time prices, so it always matches what's
  // currently in the cart.
  const estimate = estimateCartTotals(cart.lines, cart.discount);
  const hasEstimatedDiscount = estimate.discountAmount !== "0.00";

  function handleDiscountKindChange(kind: "none" | DiscountKind) {
    setDiscountKind(kind);
    if (kind === "none") {
      dispatch({ type: "setDiscount", discount: null });
      return;
    }
    dispatch({
      type: "setDiscount",
      discount: { kind, value: cart.discount?.value ?? "", reason: cart.discount?.reason ?? "" },
    });
  }

  function handleDiscountValueChange(value: string) {
    if (discountKind === "none") {
      return;
    }
    dispatch({
      type: "setDiscount",
      discount: { kind: discountKind, value, reason: cart.discount?.reason ?? "" },
    });
  }

  function handleDiscountReasonChange(reason: string) {
    if (discountKind === "none" || cart.discount == null) {
      return;
    }
    dispatch({ type: "setDiscount", discount: { ...cart.discount, reason } });
  }

  const createSale = useCreateSale();

  function handlePickLocation(location: Location) {
    setSelectedLocation(location);
    void persistLocationId(location.id);
  }

  function clearLineError(variantId: string) {
    setLineErrors((prev) => {
      if (!(variantId in prev)) {
        return prev;
      }
      const { [variantId]: _removed, ...rest } = prev;
      return rest;
    });
  }

  // Wrap every qty-changing dispatch so a line's `STOCK_INSUFFICIENT`
  // error clears as soon as the cashier tries a different quantity, rather
  // than sticking around next to a qty that might now be fine (T4 review
  // nit 7) — the server re-checks on the next "Pay" regardless.
  function handleIncrement(variantId: string) {
    dispatch({ type: "incrementQty", variantId });
    clearLineError(variantId);
  }

  function handleDecrement(variantId: string) {
    dispatch({ type: "decrementQty", variantId });
    clearLineError(variantId);
  }

  function handleRemoveLine(variantId: string) {
    dispatch({ type: "removeItem", variantId });
    clearLineError(variantId);
  }

  function handleClearCart() {
    dispatch({ type: "clear" });
    setLineErrors({});
    setGeneralError(null);
    setPossiblyRecorded(false);
    setCustomer(null);
    setDiscountKind("none");
  }

  function handlePickVariant(variant: Variant, availableQty: string, product: Product) {
    const attrs = Object.values(variant.attributes).filter(Boolean).join(" / ");
    const label = [variant.sku, attrs].filter(Boolean).join(" — ") || t("sales.items.noLabel");
    const unitPrice = resolveEffectivePrice(product, variant, timeZone);
    dispatch({
      type: "addItem",
      variantId: variant.id,
      label,
      productName: product.name,
      unitPrice,
      availableQty,
    });
    clearLineError(variant.id);
    setPickerOpen(false);
  }

  function handlePay() {
    setGeneralError(null);
    setPossiblyRecorded(false);
    if (!selectedLocation) {
      setGeneralError(t("mobile.sale.locationRequired"));
      return;
    }
    if (cart.lines.length === 0) {
      setGeneralError(t("sales.items.required"));
      return;
    }
    if (discountValueInvalid) {
      setGeneralError(t("errors.field.invalid"));
      return;
    }

    const hasDiscount =
      discountKind !== "none" &&
      cart.discount != null &&
      cart.discount.value.trim() !== "" &&
      isValidDiscountValue(discountKind, cart.discount.value) &&
      !isZeroDecimalString(cart.discount.value);

    const body: SaleCreate = {
      locationId: selectedLocation.id,
      items: cart.lines.map((line) => ({ variantId: line.variantId, qty: String(line.qty) })),
      payment: { method: paymentMethod },
      ...(customer ? { customerId: customer.id } : {}),
      ...(hasDiscount && cart.discount
        ? { discount: { type: cart.discount.kind, value: cart.discount.value.trim() } }
        : {}),
      ...(hasDiscount && cart.discount?.reason.trim()
        ? { discountReason: cart.discount.reason.trim() }
        : {}),
    };

    createSale.mutate(
      { body, idempotencyKey: cart.idempotencyKey },
      {
        onSuccess: (sale) => {
          setCompletedSale(sale);
          dispatch({ type: "completed" });
          setCustomer(null);
          setDiscountKind("none");
          setLineErrors({});
        },
        onError: (error) => {
          if (error instanceof SalesApiError) {
            if (error.code === "STOCK_INSUFFICIENT") {
              const details = error.details as StockInsufficientDetails | undefined;
              if (details?.variantId) {
                setLineErrors((prev) => ({
                  ...prev,
                  [details.variantId as string]: t("sales.errors.stockInsufficient", {
                    available: details.available ?? "0",
                  }),
                }));
                return;
              }
            }
            if (error.code === "DISCOUNT_EXCEEDS_SUBTOTAL") {
              setGeneralError(t("sales.errors.discountExceedsSubtotal"));
              return;
            }
          }
          // `idempotencyOutcome` takes the error's `code` when it was a
          // decoded server response, else `undefined` — a network drop or
          // a timeout never got as far as one, so its outcome is just as
          // unknown as `IDEMPOTENCY_KEY_REUSED`'s (`possiblyRecorded`
          // covers both), but only `IDEMPOTENCY_KEY_REUSED` actually
          // proves the *current* key was already spent, so only that case
          // mints a new one (T4 review CRITICAL: rekeying on an undecoded
          // error would let a cashier's natural retry of the same cart
          // create a second, real sale under a fresh key if the original
          // request had actually reached the server and committed —
          // `features/sales/cart.ts`'s own doc comments have the full
          // reasoning).
          const code = error instanceof SalesApiError ? error.code : undefined;
          if (idempotencyOutcome(code) === "rekey") {
            dispatch({ type: "rekey" });
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

  if (completedSale) {
    return (
      <View className="flex-1 items-center justify-center gap-4 bg-background p-6">
        <Text variant="h3">{t("sales.success.title")}</Text>
        <Text variant="large">{t("sales.success.number", { number: completedSale.number })}</Text>
        <Text>
          {t("sales.success.total", { total: formatMoney(completedSale.total, currency) })}
        </Text>
        <Button size="lg" onPress={() => setCompletedSale(null)}>
          <Text>{t("sales.success.newSale")}</Text>
        </Button>
      </View>
    );
  }

  return (
    <KeyboardAvoidingView
      behavior={Platform.OS === "ios" ? "padding" : undefined}
      className="flex-1 bg-background"
    >
      <ScrollView
        className="flex-1"
        contentContainerStyle={{ padding: 16, gap: 16 }}
        keyboardShouldPersistTaps="handled"
      >
        {generalError ? (
          <View className="gap-1 rounded-md bg-destructive/10 p-3">
            <Text className="text-destructive">{generalError}</Text>
            {possiblyRecorded ? (
              <Pressable accessibilityRole="button" onPress={() => router.push("/sale/list")}>
                <Text className="text-destructive underline">{t("mobile.sale.list.title")}</Text>
              </Pressable>
            ) : null}
          </View>
        ) : null}

        {locationsError ? (
          <View className="gap-2 rounded-md border border-border p-3">
            <Text variant="muted">{t("errors.generic")}</Text>
            <Pressable accessibilityRole="button" onPress={() => refetchLocations()}>
              <Text className="text-primary">{t("common.retry")}</Text>
            </Pressable>
          </View>
        ) : noActiveLocations ? (
          <View className="gap-2 rounded-md border border-border p-3">
            <Text variant="muted">{t("mobile.stock.noLocations")}</Text>
            <Pressable accessibilityRole="button" onPress={() => refetchLocations()}>
              <Text className="text-primary">{t("common.retry")}</Text>
            </Pressable>
          </View>
        ) : (
          <Pressable
            accessibilityRole="button"
            className="min-h-14 justify-center rounded-md border border-input bg-background px-3"
            onPress={() => setLocationModalOpen(true)}
          >
            <Text variant="small">{t("sales.fields.location")}</Text>
            <Text>{selectedLocation?.name ?? t("mobile.sale.location.placeholder")}</Text>
          </Pressable>
        )}

        <View className="gap-2">
          <View className="flex-row items-center justify-between">
            <Text variant="large">{t("sales.items.title")}</Text>
            <Button size="sm" disabled={!selectedLocation} onPress={() => setPickerOpen(true)}>
              <Plus size={16} color={tokens.color.surface} />
              <Text>{t("mobile.sale.addItem")}</Text>
            </Button>
          </View>

          {cart.lines.length === 0 ? (
            <Text variant="muted">{t("mobile.sale.cart.empty")}</Text>
          ) : (
            cart.lines.map((line) => {
              const lineError = lineErrors[line.variantId];
              const exceedsAvailable =
                !lineError && qtyExceedsAvailable(line.qty, line.availableQty);
              return (
                <View key={line.variantId} className="gap-2 rounded-md border border-border p-3">
                  <View className="flex-row items-start justify-between gap-2">
                    <View className="flex-1">
                      <Text numberOfLines={1}>{line.productName}</Text>
                      <Text variant="muted" numberOfLines={2}>
                        {line.label}
                      </Text>
                    </View>
                    <Pressable
                      accessibilityRole="button"
                      accessibilityLabel={t("sales.items.remove")}
                      className="h-9 w-9 items-center justify-center"
                      onPress={() => handleRemoveLine(line.variantId)}
                    >
                      <Trash2 size={18} color={tokens.color.danger} />
                    </Pressable>
                  </View>
                  <View className="flex-row items-center justify-between">
                    <View className="flex-row items-center gap-3">
                      <Pressable
                        accessibilityRole="button"
                        accessibilityLabel={t("mobile.sale.qty.decrease")}
                        className="h-11 w-11 items-center justify-center rounded-md border border-border active:bg-accent"
                        onPress={() => handleDecrement(line.variantId)}
                      >
                        <Minus size={20} color={tokens.color.text} />
                      </Pressable>
                      <Text variant="large" className="w-10 text-center">
                        {line.qty}
                      </Text>
                      <Pressable
                        accessibilityRole="button"
                        accessibilityLabel={t("mobile.sale.qty.increase")}
                        className="h-11 w-11 items-center justify-center rounded-md border border-border active:bg-accent"
                        onPress={() => handleIncrement(line.variantId)}
                      >
                        <Plus size={20} color={tokens.color.text} />
                      </Pressable>
                    </View>
                    <Text variant="muted">
                      {formatMoney(multiplyMoneyByQty(line.unitPrice, line.qty), currency)}
                    </Text>
                  </View>
                  {lineError ? (
                    <Text variant="small" className="text-destructive">
                      {lineError}
                    </Text>
                  ) : exceedsAvailable ? (
                    <Text variant="small" className="text-destructive">
                      {t("sales.errors.stockInsufficient", { available: line.availableQty })}
                    </Text>
                  ) : null}
                </View>
              );
            })
          )}
        </View>

        <View className="gap-2">
          <Text variant="small">{t("sales.fields.discountType")}</Text>
          <View className="flex-row gap-2">
            {DISCOUNT_KINDS.map((kind) => (
              <Button
                key={kind}
                size="sm"
                className="flex-1"
                variant={discountKind === kind ? "default" : "outline"}
                onPress={() => handleDiscountKindChange(kind)}
              >
                <Text>
                  {kind === "none"
                    ? t("mobile.sale.discount.kindNone")
                    : t(`sales.discountType.${kind}`)}
                </Text>
              </Button>
            ))}
          </View>
          {discountKind !== "none" ? (
            <>
              <Input
                keyboardType="decimal-pad"
                placeholder={t("mobile.sale.discount.valuePlaceholder")}
                value={cart.discount?.value ?? ""}
                onChangeText={handleDiscountValueChange}
              />
              {discountValueInvalid ? (
                <Text variant="small" className="text-destructive">
                  {t("errors.field.invalid")}
                </Text>
              ) : null}
              <Input
                placeholder={t("mobile.sale.discount.reasonPlaceholder")}
                value={cart.discount?.reason ?? ""}
                onChangeText={handleDiscountReasonChange}
              />
            </>
          ) : null}
        </View>

        {cart.lines.length > 0 ? (
          <View className="gap-1 rounded-md border border-border p-3">
            <Text variant="small" className="text-muted-foreground">
              {t("mobile.sale.estimate")}
            </Text>
            <View className="flex-row justify-between">
              <Text variant="muted">{t("sales.summary.subtotal")}</Text>
              <Text>{formatMoney(estimate.subtotal, currency)}</Text>
            </View>
            {hasEstimatedDiscount ? (
              <View className="flex-row justify-between">
                <Text variant="muted">{t("sales.summary.discount")}</Text>
                <Text>-{formatMoney(estimate.discountAmount, currency)}</Text>
              </View>
            ) : null}
            <View className="flex-row justify-between">
              <Text variant="large">{t("sales.summary.total")}</Text>
              <Text variant="large">{formatMoney(estimate.total, currency)}</Text>
            </View>
          </View>
        ) : null}

        <View className="gap-2">
          <Pressable
            accessibilityRole="button"
            accessibilityLabel={
              customer ? t("mobile.sale.customer.change") : t("mobile.sale.customer.attach")
            }
            className="min-h-14 justify-center rounded-md border border-input bg-background px-3"
            onPress={() => setCustomerModalOpen(true)}
          >
            <Text variant="small">{t("sales.fields.customer")}</Text>
            <Text>
              {customer
                ? [customer.fullName, customer.phone].filter(Boolean).join(" · ")
                : t("mobile.sale.customer.none")}
            </Text>
          </Pressable>
          {customer ? (
            <Pressable accessibilityRole="button" onPress={() => setCustomer(null)}>
              <Text className="text-primary">{t("sales.items.remove")}</Text>
            </Pressable>
          ) : null}
        </View>

        <View className="gap-2">
          <Text variant="small">{t("sales.fields.paymentMethod")}</Text>
          <View className="flex-row gap-2">
            {PAYMENT_METHODS.map((method) => (
              <Button
                key={method}
                size="sm"
                className="flex-1"
                variant={paymentMethod === method ? "default" : "outline"}
                onPress={() => setPaymentMethod(method)}
              >
                <Text>{t(`sales.paymentMethod.${method}`)}</Text>
              </Button>
            ))}
          </View>
        </View>

        <Pressable accessibilityRole="button" onPress={() => router.push("/sale/list")}>
          <Text className="text-center text-primary">{t("mobile.sale.list.title")}</Text>
        </Pressable>

        {cart.lines.length > 0 ? (
          <Pressable accessibilityRole="button" onPress={handleClearCart}>
            <Text className="text-center text-destructive">{t("sales.cart.clear")}</Text>
          </Pressable>
        ) : null}
      </ScrollView>

      <View className="border-border border-t bg-background p-4">
        <Button size="lg" disabled={createSale.isPending} onPress={handlePay}>
          {createSale.isPending ? (
            <>
              <ActivityIndicator color={tokens.color.surface} />
              <Text>{t("mobile.sale.paying")}</Text>
            </>
          ) : (
            <Text>{t("mobile.sale.pay")}</Text>
          )}
        </Button>
      </View>

      <LocationPickerModal
        visible={locationModalOpen}
        locations={activeLocations}
        onClose={() => setLocationModalOpen(false)}
        onPick={handlePickLocation}
      />

      <Modal visible={pickerOpen} animationType="slide" onRequestClose={() => setPickerOpen(false)}>
        <View className="flex-1 gap-2 bg-background p-4 pt-12">
          <View className="flex-row items-center justify-between">
            <Text variant="h4">{t("mobile.sale.addItem")}</Text>
            <Pressable
              accessibilityRole="button"
              onPress={() => setPickerOpen(false)}
              className="min-h-11 justify-center"
            >
              <Text className="text-primary">{t("common.cancel")}</Text>
            </Pressable>
          </View>
          {selectedLocation ? (
            <VariantPicker
              locationId={selectedLocation.id}
              excludeVariantIds={cart.lines.map((line) => line.variantId)}
              onPick={handlePickVariant}
            />
          ) : null}
        </View>
      </Modal>

      <CustomerPickerModal
        visible={customerModalOpen}
        onClose={() => setCustomerModalOpen(false)}
        onPick={setCustomer}
      />
    </KeyboardAvoidingView>
  );
}
