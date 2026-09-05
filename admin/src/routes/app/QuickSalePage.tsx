import { useMutation, useQuery } from "@tanstack/react-query";
import {
  App,
  Button,
  Card,
  Form,
  Input,
  InputNumber,
  Select,
  Space,
  Table,
  Typography,
} from "antd";
import type { ColumnsType } from "antd/es/table";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import { useMe } from "../../auth/useMe";
import { fetchProductsPage, fetchVariants, type Product, type Variant } from "../../catalog/api";
import { type Customer, fetchCustomersPage } from "../../customers/api";
import { ApiError, applyApiErrorToForm, notifyApiError } from "../../lib/errors";
import { formatMoney, formatMoneyDisplay } from "../../lib/money";
import { fetchLocationsPage } from "../../locations/api";
import {
  createSale,
  type DiscountType,
  type PaymentMethod,
  type Sale,
  type SaleCreate,
} from "../../sales/api";
import {
  clampMoneyAtZero,
  isMoneyGreaterThan,
  multiplyMoneyByQty,
  percentOfMoney,
  subtractMoney,
  sumMoney,
} from "./quick-sale/decimal";
import { persistLocationId, readStoredLocationId } from "./quick-sale/locationStorage";
import { resolveEffectivePrice } from "./quick-sale/pricing";

/** Matches every other product search in this app (`docs/05-API.md` §
 * Conventions: free-text search is ILIKE/trigram). */
const MIN_QUERY_LENGTH = 2;
const SEARCH_DEBOUNCE_MS = 300;

const PAYMENT_METHODS: PaymentMethod[] = ["cash", "card", "transfer"];
const DISCOUNT_TYPES: DiscountType[] = ["percent", "fixed"];

interface SaleFormValues {
  locationId?: string;
  customerId?: string;
  paymentMethod: PaymentMethod;
  discountType?: DiscountType;
  discountValue?: number;
  discountReason?: string;
  note?: string;
}

/** One line of the cart, staged client-side. `unitPrice` is resolved once,
 * when the line is added (`quick-sale/pricing.ts`) — a live preview only;
 * `POST /sales` sends `variantId`/`qty` and nothing else (D-56), and the
 * server is the source of truth for the price actually charged. */
interface CartLine {
  variantId: string;
  productName: string;
  variantLabel: string;
  unitPrice: string;
  qty: number;
}

/** A quick sale's `qty` as an integer decimal string — a family clothing
 * shop's till always sells whole units (this page's `InputNumber`s use
 * `precision={0}`). If a future shop needs fractional quantities at the
 * till, this is the one place to change. */
function formatSaleQty(qty: number): string {
  return String(Math.max(1, Math.trunc(qty)));
}

function variantLabel(variant: Variant, t: (key: string) => string): string {
  const attrs = Object.values(variant.attributes).filter(Boolean).join(" / ");
  return [variant.sku, attrs].filter(Boolean).join(" — ") || t("sales.items.noLabel");
}

function lineTotal(line: CartLine): string {
  return multiplyMoneyByQty(line.unitPrice, formatSaleQty(line.qty));
}

/** `409 STOCK_INSUFFICIENT details.variantId/locationId/available`
 * (`docs/05-API.md` § Conventions). */
interface StockInsufficientDetails {
  variantId?: string;
  available?: string;
}

/**
 * A cashier or manager completes a sale (`docs/03-ARCHITECTURE.md` §
 * Quick sale flow, D-52..D-57): pick a location (remembered for next
 * time), search products and add variants to a cart, optionally attach a
 * customer, apply a manual discount, choose a payment method, and submit.
 * The cart total shown here is a *preview* — the server recomputes every
 * total and rejects an insufficient-stock or over-subtotal discount
 * request (hard rule 8). Visible to every role (`docs/04-DATA-MODEL.md`
 * § 7: "Create sale, attach customer" — owner/manager/cashier).
 */
export function QuickSalePage() {
  const { t } = useTranslation();
  const { notification } = App.useApp();
  const [form] = Form.useForm<SaleFormValues>();
  // The one seed for the form's `initialValues` and its change-tracking
  // snapshot below — computed once so both agree on the starting values.
  const [initialFormValues] = useState<SaleFormValues>(() => ({
    paymentMethod: "cash",
    locationId: readStoredLocationId() ?? undefined,
  }));

  const [cart, setCart] = useState<CartLine[]>([]);
  const [lineErrors, setLineErrors] = useState<Record<string, string>>({});
  const [completedSale, setCompletedSale] = useState<Sale | null>(null);

  const { data: locationsPage } = useQuery({
    queryKey: ["locations-options"],
    queryFn: () => fetchLocationsPage(null),
  });

  // Same `GET /auth/me` data `AppLayout` reads the shop name from (D-68:
  // promo activity is a calendar-day check in the shop's own timezone, not
  // the browser's) — fall back to the browser's zone only for the brief
  // window before that query has ever resolved.
  const { data: me } = useMe();
  const shopTimeZone = me?.shop.timezone ?? Intl.DateTimeFormat().resolvedOptions().timeZone;

  // The add-item picker: search products by name/SKU, then pick a variant —
  // same shape as `PurchaseFormPage`'s items editor.
  const [searchInput, setSearchInput] = useState("");
  const [debouncedSearch, setDebouncedSearch] = useState("");
  useEffect(() => {
    const timer = setTimeout(() => setDebouncedSearch(searchInput.trim()), SEARCH_DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [searchInput]);

  const { data: productResults } = useQuery({
    queryKey: ["quick-sale-products", debouncedSearch],
    queryFn: () => fetchProductsPage({ q: debouncedSearch }, null),
    enabled: debouncedSearch.length >= MIN_QUERY_LENGTH,
  });

  const [selectedProductId, setSelectedProductId] = useState<string | undefined>(undefined);
  const { data: productVariants } = useQuery({
    queryKey: ["quick-sale-variants", selectedProductId],
    queryFn: () => fetchVariants(selectedProductId as string),
    enabled: selectedProductId != null,
  });

  const [selectedVariantId, setSelectedVariantId] = useState<string | undefined>(undefined);
  const [newQty, setNewQty] = useState<number | null>(1);

  function handleAddItem() {
    const product = productResults?.items.find(
      (candidate: Product) => candidate.id === selectedProductId,
    );
    const variant = productVariants?.find(
      (candidate: Variant) => candidate.id === selectedVariantId,
    );
    if (!product || !variant || newQty == null || newQty <= 0) {
      return;
    }
    const unitPrice = resolveEffectivePrice(product, variant, shopTimeZone);
    const label = variantLabel(variant, t);
    const addedQty = newQty;
    setCart((prev) => {
      const existingIndex = prev.findIndex((row) => row.variantId === variant.id);
      if (existingIndex >= 0) {
        // Adding a variant already in the cart increments its qty by the
        // entered amount rather than replacing it (reviewer ruling,
        // phase-4/t6b review) — matches scanning the same item twice at a
        // real till.
        const copy = [...prev];
        const existing = copy[existingIndex] as CartLine;
        copy[existingIndex] = { ...existing, qty: existing.qty + addedQty };
        return copy;
      }
      const nextLine: CartLine = {
        variantId: variant.id,
        productName: product.name,
        variantLabel: label,
        unitPrice,
        qty: addedQty,
      };
      return [...prev, nextLine];
    });
    setLineErrors((prev) => {
      if (!(variant.id in prev)) {
        return prev;
      }
      const { [variant.id]: _removed, ...rest } = prev;
      return rest;
    });
    setSelectedProductId(undefined);
    setSelectedVariantId(undefined);
    setNewQty(1);
    setSearchInput("");
  }

  function updateLineQty(variantId: string, qty: number) {
    setCart((prev) => prev.map((row) => (row.variantId === variantId ? { ...row, qty } : row)));
  }

  function removeLine(variantId: string) {
    setCart((prev) => prev.filter((row) => row.variantId !== variantId));
    setLineErrors((prev) => {
      if (!(variantId in prev)) {
        return prev;
      }
      const { [variantId]: _removed, ...rest } = prev;
      return rest;
    });
  }

  function clearCart() {
    setCart([]);
    setLineErrors({});
  }

  // Customer search — same shape as the product search above, but
  // browsable with an empty query too (`StockVariantPicker`'s pattern):
  // `q` matches `fullName` or `phone` server-side.
  const [customerSearchInput, setCustomerSearchInput] = useState("");
  const [debouncedCustomerSearch, setDebouncedCustomerSearch] = useState("");
  useEffect(() => {
    const timer = setTimeout(
      () => setDebouncedCustomerSearch(customerSearchInput.trim()),
      SEARCH_DEBOUNCE_MS,
    );
    return () => clearTimeout(timer);
  }, [customerSearchInput]);

  const { data: customerResults } = useQuery({
    queryKey: ["quick-sale-customers", debouncedCustomerSearch],
    queryFn: () => fetchCustomersPage({ q: debouncedCustomerSearch || undefined }, null),
  });

  // Mirrors the form's own values, updated synchronously by `onValuesChange`
  // below — used for the live preview and the `Idempotency-Key` signature.
  // `Form.useWatch` also works for this, but its subscription update lands
  // a render behind the field's own `onChange` (observed while reviewing
  // phase-4/t6b: a value typed right after selecting a discount type could
  // be read as stale), which is exactly the class of bug the idempotency
  // key must not have — `onValuesChange` fires as part of the same store
  // dispatch as the field's change, so this snapshot is always current.
  const [formValues, setFormValues] = useState<SaleFormValues>(initialFormValues);
  const { discountType, discountValue, discountReason } = formValues;

  const subtotal = sumMoney(cart.map(lineTotal));
  const hasDiscount = discountType != null && discountValue != null && discountValue > 0;
  const discountAmount = (() => {
    if (!hasDiscount) {
      return "0.00";
    }
    const formattedValue = formatMoney(discountValue) as string;
    return discountType === "percent" ? percentOfMoney(subtotal, formattedValue) : formattedValue;
  })();
  const discountExceedsSubtotal = isMoneyGreaterThan(discountAmount, subtotal);
  const total = clampMoneyAtZero(subtractMoney(subtotal, discountAmount));

  // The `Idempotency-Key` must change whenever the request body it will be
  // sent with changes, and stay stable across a retry of the exact same
  // body (`docs/05-API.md` § Conventions; phase-4/t6b review MAJOR 2) — so
  // fixing a qty after `STOCK_INSUFFICIENT`, or a discount after
  // `DISCOUNT_EXCEEDS_SUBTOTAL`, resubmits with a fresh key instead of
  // replaying the failed one. Derived from a signature of every field that
  // ends up in `SaleCreate`; a ref tracks the last signature a key was
  // minted for, and an effect mints a new one whenever it changes —
  // including when the cart is cleared or a sale completes, since both
  // change `items` to `[]`.
  const canonicalBodySignature = JSON.stringify({
    locationId: formValues.locationId ?? null,
    items: cart.map((row) => ({ variantId: row.variantId, qty: formatSaleQty(row.qty) })),
    discountType: hasDiscount ? discountType : null,
    discountValue: hasDiscount ? (formatMoney(discountValue) as string) : null,
    discountReason: hasDiscount ? (discountReason?.trim() ?? null) : null,
    customerId: formValues.customerId ?? null,
    paymentMethod: formValues.paymentMethod ?? null,
    note: formValues.note?.trim() || null,
  });
  const [idempotencyKey, setIdempotencyKey] = useState(() => crypto.randomUUID());
  const lastKeyedSignature = useRef(canonicalBodySignature);
  useEffect(() => {
    if (lastKeyedSignature.current === canonicalBodySignature) {
      return;
    }
    lastKeyedSignature.current = canonicalBodySignature;
    setIdempotencyKey(crypto.randomUUID());
  }, [canonicalBodySignature]);

  const createMutation = useMutation({
    mutationFn: (values: SaleFormValues) => {
      const submittedHasDiscount =
        values.discountType != null && values.discountValue != null && values.discountValue > 0;
      const body: SaleCreate = {
        locationId: values.locationId as string,
        ...(values.customerId ? { customerId: values.customerId } : {}),
        items: cart.map((row) => ({ variantId: row.variantId, qty: formatSaleQty(row.qty) })),
        ...(submittedHasDiscount
          ? {
              discount: {
                type: values.discountType as DiscountType,
                value: formatMoney(values.discountValue) as string,
              },
            }
          : {}),
        // Only sent alongside an actual discount — a reason with no
        // discount is meaningless and must never reach the server
        // (phase-4/t6b review MAJOR 1).
        ...(submittedHasDiscount && values.discountReason?.trim()
          ? { discountReason: values.discountReason.trim() }
          : {}),
        ...(values.note?.trim() ? { note: values.note.trim() } : {}),
        payment: { method: values.paymentMethod },
      };
      return createSale(body, idempotencyKey);
    },
    onSuccess: (sale) => {
      setCompletedSale(sale);
      setCart([]);
      setLineErrors({});
      form.resetFields(["customerId", "discountType", "discountValue", "discountReason", "note"]);
      setFormValues(form.getFieldsValue());
    },
    onError: (error) => {
      if (error instanceof ApiError && error.code === "STOCK_INSUFFICIENT") {
        const details = error.details as StockInsufficientDetails;
        if (details.variantId) {
          setLineErrors((prev) => ({
            ...prev,
            [details.variantId as string]: t("sales.errors.stockInsufficient", {
              available: details.available ?? "0",
            }),
          }));
          return;
        }
      }
      if (error instanceof ApiError && error.code === "DISCOUNT_EXCEEDS_SUBTOTAL") {
        form.setFields([
          { name: "discountValue", errors: [t("sales.errors.discountExceedsSubtotal")] },
        ]);
        return;
      }
      if (error instanceof ApiError && error.code === "IDEMPOTENCY_KEY_REUSED") {
        notification.error({ message: t("sales.errors.idempotencyKeyReused") });
        return;
      }
      if (!applyApiErrorToForm(form, error, t)) {
        notifyApiError(notification, error, t);
      }
    },
  });

  function handleFinish(values: SaleFormValues) {
    if (cart.length === 0) {
      notification.error({ message: t("sales.items.required") });
      return;
    }
    createMutation.mutate(values);
  }

  if (completedSale) {
    return (
      <Card title={t("sales.title")}>
        <Space direction="vertical" size="large">
          <Typography.Title level={3}>{t("sales.success.title")}</Typography.Title>
          <Typography.Text>
            {t("sales.success.number", { number: completedSale.number })}
          </Typography.Text>
          <Typography.Text>
            {t("sales.success.total", { total: formatMoneyDisplay(completedSale.total) })}
          </Typography.Text>
          <Button type="primary" onClick={() => setCompletedSale(null)}>
            {t("sales.success.newSale")}
          </Button>
        </Space>
      </Card>
    );
  }

  const cartColumns: ColumnsType<CartLine> = [
    { title: t("sales.items.columns.product"), dataIndex: "productName" },
    { title: t("sales.items.columns.variant"), dataIndex: "variantLabel" },
    {
      title: t("sales.items.columns.unitPrice"),
      key: "unitPrice",
      render: (_, row) => formatMoneyDisplay(row.unitPrice),
    },
    {
      title: t("sales.items.columns.qty"),
      key: "qty",
      render: (_, row) => (
        <Space direction="vertical" size={0}>
          <InputNumber
            aria-label={`${t("sales.items.columns.qty")}: ${row.productName} — ${row.variantLabel}`}
            min={1}
            precision={0}
            value={row.qty}
            onChange={(value) => updateLineQty(row.variantId, value ?? 1)}
          />
          {lineErrors[row.variantId] && (
            <Typography.Text type="danger" style={{ fontSize: 12 }}>
              {lineErrors[row.variantId]}
            </Typography.Text>
          )}
        </Space>
      ),
    },
    {
      title: t("sales.items.columns.lineTotal"),
      key: "lineTotal",
      render: (_, row) => formatMoneyDisplay(lineTotal(row)),
    },
    {
      title: "",
      key: "actions",
      render: (_, row) => (
        <Button size="small" danger onClick={() => removeLine(row.variantId)}>
          {t("sales.items.remove")}
        </Button>
      ),
    },
  ];

  return (
    <Card
      title={t("sales.title")}
      extra={
        <Button
          type="primary"
          disabled={cart.length === 0}
          loading={createMutation.isPending}
          onClick={() => form.submit()}
        >
          {t("sales.submit")}
        </Button>
      }
    >
      <Form<SaleFormValues>
        form={form}
        layout="vertical"
        initialValues={initialFormValues}
        onValuesChange={(_changed, allValues) => setFormValues(allValues)}
        onFinish={handleFinish}
      >
        <Form.Item
          name="locationId"
          label={t("sales.fields.location")}
          rules={[{ required: true }]}
        >
          <Select
            aria-label={t("sales.fields.location")}
            onChange={(value: string) => persistLocationId(value)}
            options={(locationsPage?.items ?? []).map((location) => ({
              value: location.id,
              label: location.name,
            }))}
          />
        </Form.Item>
        <Form.Item name="customerId" label={t("sales.fields.customer")}>
          <Select
            aria-label={t("sales.fields.customer")}
            showSearch
            allowClear
            filterOption={false}
            placeholder={t("sales.customerPlaceholder")}
            onSearch={setCustomerSearchInput}
            options={(customerResults?.items ?? []).map((customer: Customer) => ({
              value: customer.id,
              label: customer.phone
                ? `${customer.fullName} — ${customer.phone}`
                : customer.fullName,
            }))}
          />
        </Form.Item>
        <Form.Item
          name="paymentMethod"
          label={t("sales.fields.paymentMethod")}
          rules={[{ required: true }]}
        >
          <Select
            aria-label={t("sales.fields.paymentMethod")}
            options={PAYMENT_METHODS.map((method) => ({
              value: method,
              label: t(`sales.paymentMethod.${method}`),
            }))}
          />
        </Form.Item>
        <Form.Item name="discountType" label={t("sales.fields.discountType")}>
          <Select
            aria-label={t("sales.fields.discountType")}
            allowClear
            onChange={(value: DiscountType | undefined) => {
              // Clearing the discount type also clears the reason — a
              // reason with no discount is meaningless and would otherwise
              // linger in the form ready to be resurrected by a later
              // discount pick (phase-4/t6b review MAJOR 1). `setFieldValue`
              // doesn't fire `onValuesChange` (unlike a real field change),
              // so the local snapshot is updated by hand too.
              if (!value) {
                form.setFieldValue("discountReason", undefined);
                setFormValues((prev) => ({ ...prev, discountReason: undefined }));
              }
            }}
            options={DISCOUNT_TYPES.map((type) => ({
              value: type,
              label: t(`sales.discountType.${type}`),
            }))}
          />
        </Form.Item>
        <Form.Item name="discountValue" label={t("sales.fields.discountValue")}>
          <InputNumber
            aria-label={t("sales.fields.discountValue")}
            min={0}
            precision={2}
            disabled={!discountType}
            style={{ width: "100%" }}
          />
        </Form.Item>
        <Form.Item name="discountReason" label={t("sales.fields.discountReason")}>
          <Input disabled={!discountType} />
        </Form.Item>
        <Form.Item name="note" label={t("sales.fields.note")}>
          <Input.TextArea rows={2} />
        </Form.Item>
      </Form>

      <Card type="inner" title={t("sales.items.title")} style={{ marginTop: 16 }}>
        <Space style={{ marginBottom: 16 }} wrap align="start">
          <Select
            showSearch
            aria-label={t("sales.items.productPlaceholder")}
            style={{ width: 240 }}
            placeholder={t("sales.items.productPlaceholder")}
            filterOption={false}
            value={selectedProductId}
            onSearch={setSearchInput}
            onChange={(value: string) => {
              setSelectedProductId(value);
              setSelectedVariantId(undefined);
            }}
            options={(productResults?.items ?? []).map((product) => ({
              value: product.id,
              label: product.sku ? `${product.name} (${product.sku})` : product.name,
            }))}
          />
          <Select
            aria-label={t("sales.items.variantPlaceholder")}
            style={{ width: 220 }}
            placeholder={t("sales.items.variantPlaceholder")}
            disabled={!selectedProductId}
            value={selectedVariantId}
            onChange={(value: string) => setSelectedVariantId(value)}
            options={(productVariants ?? []).map((variant) => ({
              value: variant.id,
              label: variantLabel(variant, t),
            }))}
          />
          <InputNumber
            aria-label={t("sales.items.qty")}
            min={1}
            precision={0}
            placeholder={t("sales.items.qty")}
            value={newQty}
            onChange={setNewQty}
          />
          <Button
            onClick={handleAddItem}
            disabled={!selectedVariantId || newQty == null || newQty <= 0}
          >
            {t("sales.items.add")}
          </Button>
        </Space>

        <Table<CartLine>
          rowKey="variantId"
          dataSource={cart}
          pagination={false}
          locale={{ emptyText: t("sales.items.empty") }}
          columns={cartColumns}
        />
        {cart.length === 0 && (
          <p style={{ color: "#ff4d4f", marginTop: 12, marginBottom: 0 }}>
            {t("sales.items.required")}
          </p>
        )}
        {cart.length > 0 && (
          <Button onClick={clearCart} style={{ marginTop: 12 }}>
            {t("sales.cart.clear")}
          </Button>
        )}
      </Card>

      <Card type="inner" style={{ marginTop: 16 }}>
        <Space direction="vertical">
          <Typography.Text>
            {t("sales.summary.subtotal")}: {formatMoneyDisplay(subtotal)}
          </Typography.Text>
          <Typography.Text type={discountExceedsSubtotal ? "danger" : undefined}>
            {t("sales.summary.discount")}: {formatMoneyDisplay(discountAmount)}
          </Typography.Text>
          <Typography.Text strong>
            {t("sales.summary.total")}: {formatMoneyDisplay(total)}
          </Typography.Text>
        </Space>
      </Card>
    </Card>
  );
}
