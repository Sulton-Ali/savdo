import { tokens } from "@savdo/ui-tokens";
import { useQueryClient } from "@tanstack/react-query";
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
  entityOf,
  estimateCartTotals,
  generateIdempotencyKey,
  idempotencyOutcome,
  initialCartState,
  isCartReadOnly,
  isValidDiscountValue,
  multiplyMoneyByQty,
  nextAfterDraftPayError,
  planDraftPay,
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
import { draftsKeys } from "@/lib/queryKeys";
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
  const queryClient = useQueryClient();
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
  // `true` when a loaded draft's own stored location no longer names an
  // active location (deactivated or deleted since the draft was created)
  // — a boolean, not a composed message string: on a cold mount
  // `selectedLocation` can still be `null` at the exact moment this is
  // set (the location-bootstrap effect below hasn't resolved yet), so
  // composing the message there would bake in an empty name forever.
  // Rendered in JSX below instead, which reads `selectedLocation`/
  // `activeLocations` at *render* time (T14 fix round, Opus review
  // MAJOR — corrects the previous round's own fix). Cleared wherever the
  // cart returns to a state this no longer describes: a fresh draft load
  // (`applyDraftToCart`, whether or not it needs the message again),
  // `handleClearCart`/`handleDraftGoneDuringPay` (the draft link itself
  // is gone), `handlePickLocation` (the cashier just fixed it), and a
  // successful Pay/Save (the form resets to blank either way).
  const [draftLocationGone, setDraftLocationGone] = useState(false);
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
    // Inlined rather than calling the shared `handleCustomerChange` below
    // (which also marks the cart `dirty`) — a plain top-level function
    // reference would fail `useExhaustiveDependencies` (it's a new
    // identity every render, having no `useCallback` of its own; this
    // codebase doesn't use `useCallback` anywhere else either) if listed
    // as this effect's dependency.
    setCustomer({
      id: params.attachCustomerId,
      fullName: params.attachCustomerName ?? "",
      phone: params.attachCustomerPhone || null,
    });
    dispatch({ type: "markDirty" });
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
  // `null` whenever this cart isn't mid-way through applying a specific
  // draft id — reset by every place that returns the cart to that state
  // (`handleClearCart`, `handlePaymentSuccess`, `handleSaveDraft`'s
  // `onSaved`), not just set once and left alone: editing the *same*
  // draft a second time after Saving/Paying it once already on this same
  // screen instance (which stays mounted across a tab switch, T12/D-90)
  // must re-apply it, not be skipped as "already applied" (T14 fix round
  // MAJOR 4 — found live: open A -> Edit -> Save -> View draft -> Edit
  // again showed the stale "Draft saved" success view instead of A's
  // freshly reloaded form).
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
    // Narrowed once, outside the nested function below — TS's control-flow
    // narrowing of `draftIdParam` (from the guard above) doesn't carry into
    // a nested function declaration's body, only a plain local `const` in
    // this same scope does.
    const confirmedDraftId = draftIdParam;

    // Nested inside the effect (rather than a top-level function) so it
    // isn't itself an `useExhaustiveDependencies` dependency — this
    // codebase has no `useCallback` precedent to give it a stable
    // identity, and it's only ever needed from right here anyway.
    function applyDraftToCart() {
      // Only marked "applied"/cleared from the URL on the branch that
      // actually loads the draft — a cashier who cancels the
      // confirm-before-replace prompt below must still see it again the
      // next time they tap "Edit" on this same draft; marking it applied
      // (or clearing the param) unconditionally, before the prompt could
      // even be answered, silently swallowed that second "Edit" tap
      // instead (T14 fix round, Opus review MAJOR 3).
      appliedDraftIdRef.current = confirmedDraftId;
      router.setParams({ draftId: undefined });
      // This tab's screen instance stays mounted across a tab switch
      // (T12/D-90), so a still-showing success view from an earlier
      // Pay/Save on this same screen (`completedSale`/`savedDraft`)
      // would otherwise sit in front of the form this is about to fill —
      // clear both so "Edit" always lands on the loaded draft, not a
      // stale confirmation screen (found live during this task's own
      // device smoke).
      setCompletedSale(null);
      setSavedDraft(null);
      // A stale error/line-error from whatever this screen was doing
      // before "Edit" landed here must not linger over the freshly
      // loaded draft's own form (T14 fix round, Opus review MINOR).
      setGeneralError(null);
      setPossiblyRecorded(false);
      setLineErrors({});
      setDraftLocationGone(false);
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
      if (!draftLocation) {
        // The draft's own stored location has since been deactivated or
        // deleted — never fall back to `null` (the location-bootstrap
        // effect above would just silently re-select its own default the
        // very next render anyway, `selectedLocation`'s own doc comment):
        // dispatch `markDirty` right here instead — after `loadDraft`
        // above, which would otherwise overwrite it — so *whatever*
        // location this screen is already showing (the cashier's own
        // remembered/default one) is exactly what the next Pay PATCHes
        // onto the draft before completing it (`planDraftPay`), never the
        // deactivated one; the JSX below names it so the cashier can
        // change it first if that's not where this sale should book (T14
        // fix round, Opus review CRITICAL/MAJOR).
        dispatch({ type: "markDirty" });
        setDraftLocationGone(true);
      }
      setDiscountKind(draft.discount ? draft.discount.type : "none");
      setDraftCustomerId(draft.customerId);
    }

    // A cashier already mid-way through an unrelated, unsaved cart
    // (`cart.draftId` still `null`) who then opens a draft to edit — a
    // stray tap on an old "Edit" link, say — must not silently lose that
    // work (T14 fix round MINOR 10); a cart already linked to *some*
    // draft (including this very one) is safe to replace without asking,
    // same as before.
    if (cart.lines.length > 0 && !cart.draftId) {
      Alert.alert(t("mobile.drafts.replaceCartConfirm.title"), undefined, [
        {
          text: t("common.cancel"),
          style: "cancel",
          // Only the URL param clears on Cancel — `appliedDraftIdRef`
          // stays untouched, unlike the branch that actually applies the
          // draft (`applyDraftToCart`'s own comment). Clearing the param
          // (rather than leaving it) is what lets a *later* "Edit" tap on
          // this same draft re-trigger this effect at all: `draftIdParam`
          // needs a real value change (`undefined` -> the id again) to
          // re-run, since pushing the exact same id it already holds
          // wouldn't look like a change to this effect's own dependency
          // array. Clearing it also means an unrelated edit made to this
          // cart right after Cancel (adding a line, say) does not
          // re-trigger this same prompt — the effect's very first guard
          // (`!draftIdParam`) now returns before ever reaching it (T14 fix
          // round, Opus review MAJOR 3).
          onPress: () => router.setParams({ draftId: undefined }),
        },
        {
          text: t("mobile.drafts.replaceCartConfirm.replace"),
          style: "destructive",
          onPress: applyDraftToCart,
        },
      ]);
      return;
    }
    applyDraftToCart();
  }, [
    draftIdParam,
    draftQuery.data,
    locationHydrated,
    activeLocations,
    router,
    t,
    cart.lines.length,
    cart.draftId,
  ]);

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
  // Once a complete attempt has been sent for the loaded draft, every
  // editing control is disabled — only Pay (a same-key retry) and
  // Clear/Unlink stay active (`isCartReadOnly`'s own doc comment, T14 fix
  // round, Opus review CRITICAL).
  const readOnly = isCartReadOnly(cart);

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

  /** Picking a location is a deliberate change on this screen, same as
   * `handleCustomerChange` below — unconditional (harmless when no draft
   * is loaded, `CartState.dirty`'s own doc comment) rather than gated on
   * `cart.draftId`, so Pay's own `planDraftPay` PATCHes the new
   * `locationId` onto a loaded draft before completing it instead of
   * completing under the draft's stale, already-replaced location (T14
   * fix round, Opus review CRITICAL — found live: editing a loaded
   * draft's location and tapping Pay directly completed it under the
   * *old* location, since nothing had marked the cart dirty). */
  function handlePickLocation(location: Location) {
    setSelectedLocation(location);
    void persistLocationId(location.id);
    dispatch({ type: "markDirty" });
    setDraftLocationGone(false);
  }

  /** Attaches/detaches a customer as a deliberate change on this screen —
   * unlike the location/lines/discount/note fields, `customer` lives
   * outside `CartState` (a `SelectedCustomer` local to this screen, not
   * `CartState.dirty`'s own doc comment), so this dispatches `markDirty`
   * itself wherever a *user* action changes it. Never used for the
   * draft-loading effects below, which resolve a *just-loaded* draft's
   * own customer onto this same local state and must leave the freshly
   * loaded cart's `dirty: false` alone. */
  function handleCustomerChange(next: SelectedCustomer | null) {
    setCustomer(next);
    dispatch({ type: "markDirty" });
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
    setDraftLocationGone(false);
    setCustomer(null);
    setDiscountKind("none");
    // The cart is a fresh, unlinked one again — a future "Edit" on this
    // very draft id (or any other) must be able to re-apply, not be
    // skipped as "already applied" (T14 fix round MAJOR 4).
    appliedDraftIdRef.current = null;
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
    // A line loaded from a draft whose variant/product has since gone
    // inactive or been soft-deleted (`CartLine.available`'s own doc
    // comment) — both `PATCH` and `POST .../complete` fail hard on it
    // server-side, so Save/Pay are blocked client-side until it's removed
    // (T14 fix round MAJOR 3); the per-line "unavailable" tag/message
    // already names which one.
    if (cart.lines.some((line) => !line.available)) {
      setGeneralError(t("mobile.drafts.errors.itemNoLongerAvailable"));
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
      if (error.code === "NOT_FOUND") {
        // Reached only from a brand-new sale/draft (`createSale`, or
        // `createDraft` via `handleSaveDraft`'s own `onSaveError` ->
        // `handlePatchLegError` for the *update* case, never this
        // function) — `"draft"` cannot occur here (nothing in the
        // request names an existing draft), so this only ever
        // distinguishes `"location"`/`"customer"` from everything else
        // (T14 fix round, Opus review MAJOR 2). Never touches the cart
        // itself either way (hard rule: a location/customer 404 must
        // never clear or unlink it).
        const entity = entityOf(error.details);
        if (entity === "location") {
          setGeneralError(t("mobile.sale.errors.locationGone"));
          setLocationModalOpen(true);
          return;
        }
        if (entity === "customer") {
          setGeneralError(t("mobile.sale.errors.customerGone"));
          handleCustomerChange(null);
          return;
        }
        // `"variant"` here carries no line index (unlike the draft-only
        // soft `VALIDATION_FAILED` above — `resolveSaleItems`'s hard 404
        // names no specific item, this file's own `entityOf` doc
        // comment) — the same general message covers it and the
        // defensive `"other"` case.
        setGeneralError(t("mobile.drafts.errors.itemNoLongerAvailable"));
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
    setDraftLocationGone(false);
    // See `handleClearCart`'s own comment (T14 fix round MAJOR 4).
    appliedDraftIdRef.current = null;
  }

  /** Detaches the cart from a draft that Pay has just confirmed is
   * genuinely gone (`nextAfterDraftPayError`'s `"draftGone"`, either
   * leg) — keeps the lines/discount/note so the cashier may deliberately
   * re-sell the same cart, invalidates the drafts list (the vanished row)
   * and `["sales"]` (in case it was in fact *this* attempt that
   * completed it, just with the response lost), and shows the "already
   * paid or deleted" message with a link to today's sales list so the
   * cashier can check first (T14 fix round MAJOR 2). */
  function handleDraftGoneDuringPay() {
    setGeneralError(t("mobile.drafts.errors.notFound"));
    setPossiblyRecorded(true);
    setDraftLocationGone(false);
    queryClient.invalidateQueries({ queryKey: draftsKeys.all });
    queryClient.invalidateQueries({ queryKey: ["sales"] });
    dispatch({ type: "unlinkDraft", idempotencyKey: generateIdempotencyKey() });
    appliedDraftIdRef.current = null;
  }

  /** The `PATCH` leg's own error mapping for Pay-on-a-loaded-draft, via
   * `nextAfterDraftPayError` (T14 fix round). Shared with
   * `handleSaveDraft`'s own `onSaveError` below for the same two special
   * cases (`lineVariantGone`/`forbiddenToEditDraft`) — `isDraftGone`
   * lets the two callers still disagree on what "gone" does to the cart
   * (Pay keeps the lines, `handleSaveDraft` drops the link and clears —
   * its own doc comment). */
  function handlePatchLegError(error: unknown, onDraftGone: () => void) {
    const code = error instanceof SalesApiError ? error.code : undefined;
    const details = error instanceof SalesApiError ? error.details : undefined;
    const action = nextAfterDraftPayError({
      leg: "patch",
      code,
      entity: entityOf(details),
      isRetry: false,
    });
    switch (action) {
      case "lineVariantGone":
        // A line's variant/product has gone inactive/soft-deleted since
        // this cart was loaded — never seen if `formInvalid`'s own
        // unavailable-line check already caught it, so this is only a
        // narrow race (it went bad in between); the response names no
        // line index (`entityOf`'s own doc comment), so this can only be
        // a general message, not a specific tag. Never clears or unlinks
        // the cart (T14 fix round MAJOR 3).
        setGeneralError(t("mobile.drafts.errors.itemNoLongerAvailable"));
        return;
      case "locationGone":
        // The draft's own `locationId` (whatever this screen just sent)
        // no longer resolves — reopen the picker so the cashier can
        // choose another right away; never clears/unlinks the cart
        // either (T14 fix round, Opus review MAJOR 2).
        setGeneralError(t("mobile.sale.errors.locationGone"));
        setLocationModalOpen(true);
        return;
      case "customerGone":
        // Same reasoning as `locationGone` above, for `customerId` —
        // detaches the now-gone customer (`handleCustomerChange(null)`,
        // which also marks the cart dirty for the next PATCH/complete)
        // so the cashier can simply retry.
        setGeneralError(t("mobile.sale.errors.customerGone"));
        handleCustomerChange(null);
        return;
      case "draftGone":
        onDraftGone();
        return;
      case "forbiddenToEditDraft":
        setGeneralError(t("mobile.drafts.errors.forbiddenEdit"));
        return;
      case "patchNetworkSafe":
        // A `PATCH` is naturally idempotent (it just re-replaces the same
        // target state), so — unlike `complete` — a lost response here is
        // safe to retry without any "might already be recorded" warning
        // (T14 fix round MINOR 5): `possiblyRecorded` stays untouched.
        setGeneralError(t("errors.generic"));
        return;
      default:
        // "patchGenericError" — VALIDATION_FAILED (e.g. `items` too_long,
        // vanishingly unlikely mid-edit) or DISCOUNT_EXCEEDS_SUBTOTAL.
        if (code === "DISCOUNT_EXCEEDS_SUBTOTAL") {
          setGeneralError(t("sales.errors.discountExceedsSubtotal"));
          return;
        }
        setGeneralError(t("errors.generic"));
    }
  }

  /** The `complete` leg's own error mapping for Pay (plain or
   * on-a-loaded-draft), via `nextAfterDraftPayError` (T14 fix round).
   * `isRetry` is `true` only for the response to the one-shot same-key
   * replay `"retryCompleteSameKey"` itself triggers. Every code this
   * doesn't special-case (`STOCK_INSUFFICIENT`, the unavailable-line
   * `VALIDATION_FAILED`, `DISCOUNT_EXCEEDS_SUBTOTAL`,
   * `IDEMPOTENCY_KEY_REUSED`, a network drop otherwise unclassified)
   * defers to the existing `handlePaymentError`, unchanged. */
  function handleCompleteLegError(error: unknown, isRetry: boolean) {
    const code = error instanceof SalesApiError ? error.code : undefined;
    const details = error instanceof SalesApiError ? error.details : undefined;
    // A decoded server response — success or failure — is the server's
    // own final answer, never an unknown outcome: lift the read-only
    // state right away so the cashier can fix whatever the error named
    // (a gone customer/location, a bad line, an over-large discount, …)
    // and Pay again, which will PATCH the fix in before completing
    // (`planDraftPay`, once whichever fix-up below marks the cart dirty)
    // — T14 fix round, Opus review MAJOR 1. Only an *undecoded* network
    // failure (`code === undefined`) leaves `completionAttempted` `true`,
    // since that request's outcome genuinely isn't known yet.
    if (code !== undefined) {
      dispatch({ type: "completionOutcomeKnown" });
    }
    const action = nextAfterDraftPayError({
      leg: "complete",
      code,
      // `CompleteSaleDraftTx` hands the draft's stored location/customer
      // straight to `CreateSaleTx`, so a `NOT_FOUND` here can name
      // `"location"`/`"customer"` (gone since the draft was created or
      // last edited) just as much as `PATCH` can, not only `"draft"`
      // (T14 fix round, Opus review MAJOR 2 — the hard-coded `false`
      // this replaced could never have reported either).
      entity: entityOf(details),
      isRetry,
    });
    switch (action) {
      case "retryCompleteSameKey":
        sendDraftComplete(true);
        return;
      case "draftGone":
        handleDraftGoneDuringPay();
        return;
      case "lineVariantGone":
        setGeneralError(t("mobile.drafts.errors.itemNoLongerAvailable"));
        return;
      case "locationGone":
        setGeneralError(t("mobile.sale.errors.locationGone"));
        setLocationModalOpen(true);
        return;
      case "customerGone":
        setGeneralError(t("mobile.sale.errors.customerGone"));
        handleCustomerChange(null);
        return;
      case "completeNetworkAmbiguous":
        setGeneralError(t("mobile.sale.errors.networkUnknown"));
        setPossiblyRecorded(true);
        return;
      default:
        handlePaymentError(error);
    }
  }

  /** Sends `POST /sales/drafts/{id}/complete` for the currently loaded
   * draft under the cart's own `idempotencyKey` — used both for the
   * first attempt and (`isRetry: true`) for `handleCompleteLegError`'s
   * one-shot same-key replay after a `NOT_FOUND`. Marks
   * `completionAttempted` the moment the request is sent, before its
   * response arrives (`CartState.completionAttempted`'s own doc comment,
   * T14 fix round CRITICAL) — idempotent to dispatch twice, so the retry
   * calling this again is harmless. */
  function sendDraftComplete(isRetry: boolean) {
    if (!cart.draftId) {
      return;
    }
    const draftId = cart.draftId;
    dispatch({ type: "completionAttempted" });
    completeDraft.mutate(
      { id: draftId, body: { paymentMethod }, idempotencyKey: cart.idempotencyKey },
      {
        onSuccess: handlePaymentSuccess,
        onError: (error) => handleCompleteLegError(error, isRetry),
      },
    );
  }

  function handlePay() {
    setGeneralError(null);
    setPossiblyRecorded(false);
    if (formInvalid() || !selectedLocation) {
      return;
    }

    // Editing a loaded draft (T14/D-87, fix round CRITICAL): `planDraftPay`
    // decides whether this needs a `PATCH` first — only when the cart has
    // changed since it was loaded (or since the last successful Save/Pay)
    // *and* no complete attempt for it has been sent yet. Once a complete
    // attempt has been sent, every further Pay tap replays `complete`
    // directly under the *same* `Idempotency-Key`, never PATCHing again —
    // `planDraftPay`'s and `CartState.completionAttempted`'s own doc
    // comments have the full reasoning (a PATCH after an attempt whose
    // outcome is unknown could race with, or paper over, a complete that
    // already committed).
    if (cart.draftId) {
      const draftId = cart.draftId;
      const plan = planDraftPay({
        draftId,
        dirty: cart.dirty,
        completionAttempted: cart.completionAttempted,
      });
      if (plan === "patchThenComplete") {
        const patchBody = buildDraftPatchBody(cart, selectedLocation.id, customer?.id ?? null);
        updateDraft.mutate(
          { id: draftId, body: patchBody },
          {
            onSuccess: () => sendDraftComplete(false),
            onError: (error) => handlePatchLegError(error, handleDraftGoneDuringPay),
          },
        );
        return;
      }
      sendDraftComplete(false);
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
      setDraftLocationGone(false);
      appliedDraftIdRef.current = null;
    }

    // Shares `handlePatchLegError`'s leg mapping with `handlePay` (T14 fix
    // round item 3: a `404` naming an unavailable variant —
    // `isVariantNotFoundDetails` — must never be treated as "the draft
    // itself is gone"). Unlike Pay's own `handleDraftGoneDuringPay`, a
    // genuinely-gone draft here drops the link and clears the cart
    // outright (unchanged from before this fix round) — nothing left to
    // PATCH, so the cashier starts a brand-new draft instead of resuming
    // one that no longer exists.
    function onSaveError(error: unknown) {
      handlePatchLegError(error, () => {
        setGeneralError(t("mobile.drafts.errors.notFound"));
        dispatch({ type: "clear" });
        setDraftLocationGone(false);
        appliedDraftIdRef.current = null;
      });
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

        {draftLocationGone ? (
          <View className="gap-1 rounded-md bg-destructive/10 p-3">
            <Text className="text-destructive">
              {t("mobile.sale.errors.draftLocationGone", {
                // Read at *render* time, never composed once inside the
                // draft-loading effect (`draftLocationGone`'s own doc
                // comment) — `selectedLocation` may still be `null` on a
                // cold mount when the flag is first set, in which case
                // this falls back to the same default the location-
                // bootstrap effect above would itself pick (its default,
                // else its first active location) rather than ever
                // showing an empty name.
                location:
                  selectedLocation?.name ??
                  activeLocations.find((l) => l.isDefault)?.name ??
                  activeLocations[0]?.name ??
                  "",
              })}
            </Text>
          </View>
        ) : null}

        {readOnly ? (
          <View className="gap-1 rounded-md border border-border bg-muted/30 p-3">
            <Text>{t("mobile.sale.readOnlyBanner")}</Text>
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
            disabled={readOnly}
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
            <Button
              size="sm"
              disabled={!selectedLocation || readOnly}
              onPress={() => setPickerOpen(true)}
            >
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
                    {!line.available ? (
                      <Text variant="small" className="text-destructive">
                        {t("mobile.drafts.detail.unavailable")}
                      </Text>
                    ) : null}
                    <Pressable
                      accessibilityRole="button"
                      accessibilityLabel={t("sales.items.remove")}
                      disabled={readOnly}
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
                        disabled={readOnly}
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
                        disabled={readOnly}
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
                  {!line.available ? (
                    <View className="gap-1">
                      <Text variant="small" className="text-destructive">
                        {t("mobile.drafts.errors.lineUnavailable", { name: line.productName })}
                      </Text>
                      <Pressable
                        accessibilityRole="button"
                        disabled={readOnly}
                        onPress={() => handleRemoveLine(line.variantId)}
                      >
                        <Text className="text-primary">{t("sales.items.remove")}</Text>
                      </Pressable>
                    </View>
                  ) : lineError ? (
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
                disabled={readOnly}
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
                editable={!readOnly}
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
                editable={!readOnly}
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
            disabled={readOnly}
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
            <Pressable
              accessibilityRole="button"
              disabled={readOnly}
              onPress={() => handleCustomerChange(null)}
            >
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
                // Changing the payment method mid-read-only would change
                // the body a same-key `complete` retry sends — the exact
                // "same request under the same key" the idempotent replay
                // (`sendDraftComplete`) relies on to safely resolve an
                // attempt whose outcome is unknown (T14 fix round, Opus
                // review MINOR).
                disabled={readOnly}
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
            editable={!readOnly}
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
          disabled={createSale.isPending || updateDraft.isPending || completeDraft.isPending}
          onPress={handlePay}
        >
          {createSale.isPending || updateDraft.isPending || completeDraft.isPending ? (
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
              // Save is a form of editing (a `PATCH`, for a loaded draft)
              // — disabled once read-only for the same reason every other
              // editing control on this screen is (`readOnly`'s own doc
              // comment): only Pay and Clear/Unlink stay active once a
              // complete attempt has been sent.
              disabled={createDraft.isPending || updateDraft.isPending || readOnly}
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
        onPick={handleCustomerChange}
      />
    </KeyboardAvoidingView>
  );
}
