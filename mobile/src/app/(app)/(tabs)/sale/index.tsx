import { tokens } from "@savdo/ui-tokens";
import { useLocalSearchParams, useRouter } from "expo-router";
import { Minus, Plus, Trash2, UserPlus } from "lucide-react-native";
import { useEffect, useMemo, useReducer, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  ActivityIndicator,
  Alert,
  FlatList,
  KeyboardAvoidingView,
  Modal,
  Platform,
  Pressable,
  ScrollView,
  TextInput,
  View,
} from "react-native";
import { SafeAreaView, useSafeAreaInsets } from "react-native-safe-area-context";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Text } from "@/components/ui/text";
import type { Location, Product, Variant } from "@/features/catalog/api";
import { useDebouncedValue, useLocations } from "@/features/catalog/hooks";
import { resolveEffectivePrice } from "@/features/catalog/pricing";
import { VariantPicker } from "@/features/catalog/VariantPicker";
import type { Customer } from "@/features/customers/api";
import { useCustomer, useCustomersSearch } from "@/features/customers/hooks";
import {
  type PaymentMethod,
  type Sale,
  type SaleCreate,
  SalesApiError,
} from "@/features/sales/api";
import {
  buildDraftCreateBody,
  buildDraftPatchBody,
  cartDiscountFromDraft,
  cartLinesFromDraftItems,
  cartLinesToSaleItems,
  cartReducer,
  type DiscountKind,
  estimateCartTotals,
  generateIdempotencyKey,
  idempotencyOutcome,
  initialCartState,
  isValidDiscountValue,
  multiplyMoneyByQty,
  qtyExceedsAvailable,
  resolveSubmitDiscount,
  resolveSubmitDiscountReason,
} from "@/features/sales/cart";
import { parseUnavailableLineIndexes } from "@/features/sales/drafts";
import {
  useCompleteSaleDraft,
  useCreateSale,
  useCreateSaleDraft,
  useDeleteSaleDraft,
  useDraft,
  useUpdateSaleDraft,
} from "@/features/sales/hooks";
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
  const { data, isFetching, fetchNextPage, hasNextPage, isFetchingNextPage } =
    useCustomersSearch(debouncedQuery);
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
      <SafeAreaView className="flex-1 gap-3 bg-background p-4" edges={["top", "bottom"]}>
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
          onEndReachedThreshold={0.4}
          onEndReached={() => {
            if (hasNextPage && !isFetchingNextPage) {
              fetchNextPage();
            }
          }}
          ListEmptyComponent={
            !isFetching ? (
              <Text variant="muted" className="p-4 text-center">
                {t("mobile.customers.list.empty")}
              </Text>
            ) : null
          }
          ListFooterComponent={isFetchingNextPage ? <ActivityIndicator className="py-4" /> : null}
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
      </SafeAreaView>
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
 *
 * A `401` mid-cart (a revoked/expired session) clears the cart by design:
 * `lib/api.ts`'s middleware calls `resetSessionCache` on every `401`, which
 * removes every non-session cached query and bounces the root layout's auth
 * gate to the login screen (ADR-010 — no cross-session data may survive a
 * session boundary); this screen keeps its cart in local `useReducer` state
 * with no persistence of its own, so unmounting behind that gate loses an
 * in-progress, unpaid cart along with it. Accepted rather than fixed here:
 * persisting an unsubmitted cart across a session boundary would risk the
 * next session (a different cashier, say) resuming someone else's
 * in-progress sale.
 */
export default function SaleScreen() {
  const { t } = useTranslation();
  const router = useRouter();
  const insets = useSafeAreaInsets();
  const { shop } = useSession();
  const currency = shop?.currency ?? "UZS";
  const timeZone = shop?.timezone ?? Intl.DateTimeFormat().resolvedOptions().timeZone;
  const params = useLocalSearchParams<{
    attachCustomerId?: string;
    attachCustomerName?: string;
    attachCustomerPhone?: string;
    draftId?: string;
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
  // Set once `Save draft` succeeds (create or update) — a small success
  // view, mirroring `completedSale` above, with a link to the saved draft
  // instead of a sale number/total (T14 deliverable 1).
  const [savedDraft, setSavedDraft] = useState<{ id: string } | null>(null);

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

  // Edit a draft (T14): `drafts/[id].tsx`'s "Edit" pushes here with
  // `?draftId=`. Waits for both the draft itself (`GET
  // /sales/drafts/{id}`) and the shop's locations to have loaded — the
  // latter so `draft.locationId` can be resolved to an actual `Location`
  // the location picker knows about — then replaces the whole cart via
  // `loadDraft` and applies the draft's location/customer/discount-kind
  // onto this screen's own local state the same way the cart doesn't own
  // any of those three. Applied at most once per draft id (mirrors the
  // `attachCustomerId` effect above); the remembered-location bootstrap
  // effect above may have already set a *default* `selectedLocation` by
  // the time this runs — this deliberately overrides it with the draft's
  // own location regardless, without persisting it as the new remembered
  // default (editing someone else's draft must not change what a
  // cashier's own next fresh sale defaults to).
  const draftIdParam = params.draftId;
  const draftQuery = useDraft(draftIdParam);
  const appliedDraftIdRef = useRef<string | null>(null);
  const [draftCustomerId, setDraftCustomerId] = useState<string | null>(null);
  const draftCustomerQuery = useCustomer(draftCustomerId ?? undefined);
  useEffect(() => {
    if (!draftIdParam || draftIdParam === appliedDraftIdRef.current) {
      return;
    }
    if (!draftQuery.data || !locationHydrated || activeLocations.length === 0) {
      return;
    }
    const draft = draftQuery.data;
    appliedDraftIdRef.current = draftIdParam;
    // This tab's screen instance stays mounted across a tab switch (T12/
    // D-90), so a still-showing success view from an earlier Pay/Save on
    // this same screen (`completedSale`/`savedDraft`) would otherwise sit
    // in front of the form this effect is about to fill — clear both so
    // "Edit" always lands on the loaded draft, not a stale confirmation
    // screen (found live during this task's own device smoke).
    setCompletedSale(null);
    setSavedDraft(null);
    const draftLocation = activeLocations.find((l) => l.id === draft.locationId);
    if (draftLocation) {
      setSelectedLocation(draftLocation);
    }
    dispatch({
      type: "loadDraft",
      draftId: draft.id,
      lines: cartLinesFromDraftItems(draft.items),
      discount: cartDiscountFromDraft(draft.discount, draft.discountReason),
      note: draft.note ?? "",
      idempotencyKey: generateIdempotencyKey(),
    });
    setDiscountKind(draft.discount ? draft.discount.type : "none");
    setDraftCustomerId(draft.customerId);
    router.setParams({ draftId: undefined });
  }, [draftIdParam, draftQuery.data, locationHydrated, activeLocations, router]);

  // `SaleDraft.customerId` is only an id — this resolves it to a display
  // name/phone for the customer row, same as `attachCustomerId` above.
  useEffect(() => {
    if (draftCustomerQuery.data) {
      setCustomer({
        id: draftCustomerQuery.data.id,
        fullName: draftCustomerQuery.data.fullName,
        phone: draftCustomerQuery.data.phone,
      });
    }
  }, [draftCustomerQuery.data]);

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
  const createDraft = useCreateSaleDraft();
  const updateDraft = useUpdateSaleDraft();
  const deleteDraft = useDeleteSaleDraft();
  const completeDraft = useCompleteSaleDraft();

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
      // Only adopted by the reducer if this fills an empty cart
      // (`cartReducer`'s own doc comment) — minted here regardless since
      // the reducer, not this call site, decides whether it's needed.
      idempotencyKey: generateIdempotencyKey(),
    });
    clearLineError(variant.id);
    setPickerOpen(false);
  }

  /** Shared by `handlePay` and `handleSaveDraft`: the location/cart-empty/
   * discount-format checks every submit needs before building a body,
   * `true` (and an error already set) when the form isn't ready. */
  function formInvalid(): boolean {
    if (!selectedLocation) {
      setGeneralError(t("mobile.sale.locationRequired"));
      return true;
    }
    if (cart.lines.length === 0) {
      setGeneralError(t("sales.items.required"));
      return true;
    }
    if (discountValueInvalid) {
      setGeneralError(t("errors.field.invalid"));
      return true;
    }
    return false;
  }

  /** Maps a `POST .../complete` error the same way for both the plain
   * "Pay" flow and the "editing a loaded draft" Pay flow below — the
   * error shapes are identical (`docs/05-API.md`'s complete row) even
   * though one call is `POST /sales` and the other is `POST
   * /sales/drafts/{id}/complete`. */
  function handlePaymentError(error: unknown) {
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
      if (error.code === "VALIDATION_FAILED") {
        // Only reachable completing a loaded draft (D-88: a line has gone
        // unavailable since it was added) — `items[<i>].variantId` names
        // an index into the body just sent, which is `cart.lines` in the
        // same order (`cartLinesToSaleItems`), so it maps straight back to
        // that line's `variantId`.
        const fields = (error.details as { fields?: Record<string, string> } | undefined)?.fields;
        const indexes = parseUnavailableLineIndexes(fields);
        if (indexes.length > 0) {
          setLineErrors((prev) => {
            const next = { ...prev };
            for (const index of indexes) {
              const line = cart.lines[index];
              if (line) {
                next[line.variantId] = t("mobile.drafts.errors.lineUnavailable", {
                  name: line.productName,
                });
              }
            }
            return next;
          });
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
      dispatch({ type: "rekey", idempotencyKey: generateIdempotencyKey() });
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
  }

  function handlePaymentSuccess(sale: Sale) {
    setCompletedSale(sale);
    dispatch({ type: "completed" });
    setCustomer(null);
    setDiscountKind("none");
    setLineErrors({});
  }

  function handlePay() {
    setGeneralError(null);
    setPossiblyRecorded(false);
    if (formInvalid() || !selectedLocation) {
      return;
    }

    // Editing a loaded draft (T14/D-87): Pay first replaces the draft's
    // server-side state with whatever this screen currently shows
    // (`buildDraftPatchBody`, same body `handleSaveDraft` would send),
    // then completes it — so a qty/discount/customer change made on this
    // screen before tapping Pay is never silently lost, and the draft
    // itself is deleted server-side by the same completion (D-87)
    // regardless of whether the patch changed anything. The `PATCH` has no
    // `Idempotency-Key` of its own (not in the contract; a duplicate PATCH
    // is naturally idempotent, it just re-replaces the same state) — only
    // `.../complete` carries `cart.idempotencyKey`, exactly the semantics
    // `createSale` already uses.
    if (cart.draftId) {
      const draftId = cart.draftId;
      const patchBody = buildDraftPatchBody(cart, selectedLocation.id, customer?.id ?? null);
      updateDraft.mutate(
        { id: draftId, body: patchBody },
        {
          onSuccess: () => {
            completeDraft.mutate(
              { id: draftId, body: { paymentMethod }, idempotencyKey: cart.idempotencyKey },
              { onSuccess: handlePaymentSuccess, onError: handlePaymentError },
            );
          },
          onError: handlePaymentError,
        },
      );
      return;
    }

    const body: SaleCreate = {
      locationId: selectedLocation.id,
      items: cartLinesToSaleItems(cart.lines),
      payment: { method: paymentMethod },
      ...(customer ? { customerId: customer.id } : {}),
      ...(() => {
        const discount = resolveSubmitDiscount(cart.discount);
        return discount ? { discount } : {};
      })(),
      ...(() => {
        const discountReason = resolveSubmitDiscountReason(cart.discount);
        return discountReason ? { discountReason } : {};
      })(),
      ...(cart.note.trim() ? { note: cart.note.trim() } : {}),
    };

    createSale.mutate(
      { body, idempotencyKey: cart.idempotencyKey },
      { onSuccess: handlePaymentSuccess, onError: handlePaymentError },
    );
  }

  /** `Save draft` (T14 deliverable 1): creates a new draft, or — while
   * editing a loaded one (`cart.draftId` set) — replaces its server-side
   * state via `PATCH`. Either way the cart clears afterwards and
   * `savedDraft` shows a small success view with a link to it, mirroring
   * `completedSale`'s Pay confirmation. */
  function handleSaveDraft() {
    setGeneralError(null);
    setPossiblyRecorded(false);
    if (formInvalid() || !selectedLocation) {
      return;
    }

    function onSaved(draft: { id: string }) {
      setSavedDraft({ id: draft.id });
      dispatch({ type: "completed" });
      setCustomer(null);
      setDiscountKind("none");
      setLineErrors({});
    }

    function onSaveError(error: unknown) {
      if (error instanceof SalesApiError) {
        if (error.code === "DISCOUNT_EXCEEDS_SUBTOTAL") {
          setGeneralError(t("sales.errors.discountExceedsSubtotal"));
          return;
        }
        if (error.code === "FORBIDDEN") {
          setGeneralError(t("errors.forbidden"));
          return;
        }
        if (error.code === "NOT_FOUND") {
          // The draft being edited was completed/deleted elsewhere in the
          // meantime — nothing left to PATCH; drop the link to it and let
          // the cashier save a brand-new draft instead.
          setGeneralError(t("mobile.drafts.errors.notFound"));
          dispatch({ type: "clear" });
          return;
        }
      }
      setGeneralError(t("errors.generic"));
    }

    if (cart.draftId) {
      const body = buildDraftPatchBody(cart, selectedLocation.id, customer?.id ?? null);
      updateDraft.mutate({ id: cart.draftId, body }, { onSuccess: onSaved, onError: onSaveError });
      return;
    }

    const body = buildDraftCreateBody(cart, selectedLocation.id, customer?.id ?? null);
    createDraft.mutate(body, { onSuccess: onSaved, onError: onSaveError });
  }

  /** `Delete` (T14 deliverable 1): clears the cart, confirmed first. While
   * editing a loaded draft (`cart.draftId` set), asks whether to delete
   * the draft on the server too or only discard this screen's local
   * changes (this task's own brief) — either way the local cart clears. */
  function handleDeletePress() {
    if (cart.draftId) {
      const draftId = cart.draftId;
      Alert.alert(t("mobile.drafts.discardConfirm.title"), undefined, [
        { text: t("common.cancel"), style: "cancel" },
        {
          text: t("mobile.drafts.discardConfirm.discardLocal"),
          onPress: handleClearCart,
        },
        {
          text: t("mobile.drafts.discardConfirm.deleteServer"),
          style: "destructive",
          onPress: () => {
            deleteDraft.mutate(draftId, {
              onSuccess: handleClearCart,
              onError: (error) => {
                if (error instanceof SalesApiError && error.code === "NOT_FOUND") {
                  // Already gone — the local cart still needs clearing.
                  handleClearCart();
                  return;
                }
                setGeneralError(t("errors.generic"));
              },
            });
          },
        },
      ]);
      return;
    }
    Alert.alert(t("sales.cart.clear"), t("mobile.sale.clearConfirm"), [
      { text: t("common.cancel"), style: "cancel" },
      { text: t("mobile.drafts.actions.delete"), style: "destructive", onPress: handleClearCart },
    ]);
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

  if (savedDraft) {
    return (
      <View className="flex-1 items-center justify-center gap-4 bg-background p-6">
        <Text variant="h3">{t("mobile.drafts.saved.title")}</Text>
        <Button size="lg" onPress={() => router.push(`/drafts/${savedDraft.id}`)}>
          <Text>{t("mobile.drafts.saved.view")}</Text>
        </Button>
        <Button size="lg" variant="outline" onPress={() => setSavedDraft(null)}>
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
              <Pressable accessibilityRole="button" onPress={() => router.push("/sales")}>
                <Text className="text-destructive underline">{t("mobile.sale.list.link")}</Text>
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
          <View className="flex-row flex-wrap gap-2">
            {DISCOUNT_KINDS.map((kind) => (
              <Button
                key={kind}
                size="sm"
                className="h-auto min-h-9 flex-1 py-2"
                variant={discountKind === kind ? "default" : "outline"}
                onPress={() => handleDiscountKindChange(kind)}
              >
                <Text className="text-center">
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
          <View className="flex-row flex-wrap gap-2">
            {PAYMENT_METHODS.map((method) => (
              <Button
                key={method}
                size="sm"
                className="h-auto min-h-9 flex-1 py-2"
                variant={paymentMethod === method ? "default" : "outline"}
                onPress={() => setPaymentMethod(method)}
              >
                <Text className="text-center">{t(`sales.paymentMethod.${method}`)}</Text>
              </Button>
            ))}
          </View>
        </View>

        <View className="gap-2">
          <Text variant="small">{t("sales.fields.note")}</Text>
          <Input
            placeholder={t("mobile.sale.notePlaceholder")}
            value={cart.note}
            onChangeText={(note) => dispatch({ type: "setNote", note })}
          />
        </View>

        <Pressable accessibilityRole="button" onPress={() => router.push("/sales")}>
          <Text className="text-center text-primary">{t("mobile.sale.list.link")}</Text>
        </Pressable>
      </ScrollView>

      <View
        className="gap-2 border-border border-t bg-background px-4 pt-4"
        style={{ paddingBottom: Math.max(insets.bottom, 16) }}
      >
        <Button
          size="lg"
          disabled={createSale.isPending || completeDraft.isPending}
          onPress={handlePay}
        >
          {createSale.isPending || completeDraft.isPending ? (
            <>
              <ActivityIndicator color={tokens.color.surface} />
              <Text>{t("mobile.sale.paying")}</Text>
            </>
          ) : (
            <Text>{t("mobile.sale.pay")}</Text>
          )}
        </Button>
        {cart.lines.length > 0 ? (
          <View className="flex-row gap-2">
            <Button
              variant="outline"
              className="flex-1"
              disabled={createDraft.isPending || updateDraft.isPending}
              onPress={handleSaveDraft}
            >
              <Text>
                {cart.draftId
                  ? t("mobile.drafts.actions.save")
                  : t("mobile.drafts.actions.saveDraft")}
              </Text>
            </Button>
            <Button
              variant="destructive"
              className="flex-1"
              disabled={deleteDraft.isPending}
              onPress={handleDeletePress}
            >
              <Text>{t("mobile.drafts.actions.delete")}</Text>
            </Button>
          </View>
        ) : null}
      </View>

      <LocationPickerModal
        visible={locationModalOpen}
        locations={activeLocations}
        onClose={() => setLocationModalOpen(false)}
        onPick={handlePickLocation}
      />

      <Modal visible={pickerOpen} animationType="slide" onRequestClose={() => setPickerOpen(false)}>
        <SafeAreaView className="flex-1 gap-2 bg-background p-4" edges={["top", "bottom"]}>
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
        </SafeAreaView>
      </Modal>

      <CustomerPickerModal
        visible={customerModalOpen}
        onClose={() => setCustomerModalOpen(false)}
        onPick={setCustomer}
      />
    </KeyboardAvoidingView>
  );
}
