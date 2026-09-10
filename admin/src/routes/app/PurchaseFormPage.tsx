import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import {
  App,
  Breadcrumb,
  Button,
  Card,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Select,
  Skeleton,
  Space,
  Table,
  Typography,
} from "antd";
import type { ColumnsType } from "antd/es/table";
import { ArrowLeft } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import { fetchProductsPage, fetchVariants, type Variant } from "../../catalog/api";
import { FormGrid } from "../../components/FormGrid";
import { ApiError, applyApiErrorToForm, notifyApiError } from "../../lib/errors";
import { formatMoney, parseMoney } from "../../lib/money";
import { fetchLocationsPage } from "../../locations/api";
import {
  cancelPurchase,
  createPurchase,
  fetchPurchase,
  type PurchaseCreate,
  type PurchaseItemCreate,
  type PurchasePatch,
  receivePurchase,
  updatePurchase,
} from "../../purchases/api";
import { fetchSuppliersPage } from "../../suppliers/api";

/** Matches `ProductsListPage`'s search minimum (`docs/05-API.md` §
 * Conventions: free-text search is ILIKE/trigram). */
const MIN_QUERY_LENGTH = 2;
const SEARCH_DEBOUNCE_MS = 300;

interface PurchaseMainFormValues {
  supplierId?: string;
  locationId?: string;
  supplierInvoiceNo?: string;
  note?: string;
}

/** One row of the items editor, staged client-side. `productName`/
 * `variantLabel`/`sku` are display-only — the wire shape
 * (`PurchaseItemCreate`) only ever carries `variantId`/`qty`/`unitCost`. For
 * a purchase loaded from the server they come straight off `PurchaseItem`
 * (read-only fields added alongside T4's purchases endpoints); for an item
 * added in this session, before the purchase is saved, they come from the
 * product/variant search instead. */
interface ItemRow {
  variantId: string;
  productName: string;
  variantLabel: string;
  sku: string | null;
  qty: number;
  unitCost: number;
}

/** Quantities are decimal strings with 3 places on the wire (`docs/05-API.md`
 * § Conventions example: `"available": "2.000"`) — money uses `lib/money`'s
 * 2-place `formatMoney`/`parseMoney` instead. */
function formatQty(value: number): string {
  return value.toFixed(3);
}

function parseQty(value: string): number {
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : 0;
}

function variantLabel(variant: Variant, t: (key: string) => string): string {
  const attrs = Object.values(variant.attributes).filter(Boolean).join(" / ");
  return [variant.sku, attrs].filter(Boolean).join(" — ") || t("purchases.form.items.noLabel");
}

/** Shared by `purchaseNewRoute` (`purchaseId` undefined) and
 * `purchaseEditRoute` (`purchaseId` set). Editing — the supplier/location/
 * invoice/note fields and the items editor — is only enabled while the
 * purchase is a draft (or always, in create mode); a `received` or
 * `cancelled` purchase is read-only here, per `PATCH /purchases/{id}`'s
 * `409 PURCHASE_NOT_DRAFT`. */
export function PurchaseFormPage({ purchaseId }: { purchaseId?: string }) {
  const { t } = useTranslation();
  const { notification } = App.useApp();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [form] = Form.useForm<PurchaseMainFormValues>();

  const isEdit = purchaseId != null;

  const { data: purchase, isPending: purchasePending } = useQuery({
    queryKey: ["purchase", purchaseId],
    queryFn: () => fetchPurchase(purchaseId as string),
    enabled: isEdit,
  });

  // Neither list is cursor-paginated here — a small shop's supplier/
  // location lists are short, same approach as other Select-backed filters
  // in this app (e.g. `ProductFormPage`'s unit/category lists).
  const { data: suppliersPage } = useQuery({
    queryKey: ["suppliers-options"],
    queryFn: () => fetchSuppliersPage({}, null),
  });
  const { data: locationsPage } = useQuery({
    queryKey: ["locations-options"],
    queryFn: () => fetchLocationsPage(null),
  });

  const editable = !isEdit || purchase?.status === "draft";

  // Display labels for a variantId, resolved as the user searches products
  // to add an item in this session. `PurchaseItem` itself now carries
  // read-only `productName`/`variantLabel`/`sku` (T4), so this cache only
  // ever matters for an item added this session that isn't in the server's
  // `purchase.items` yet (e.g. before the first save) — it's a fallback,
  // not the primary source.
  const [resolvedLabels, setResolvedLabels] = useState<
    Record<string, { productName: string; variantLabel: string }>
  >({});

  const [items, setItems] = useState<ItemRow[]>([]);
  const [itemsInitialized, setItemsInitialized] = useState(false);
  useEffect(() => {
    if (!isEdit || !purchase || itemsInitialized) {
      return;
    }
    setItems(
      purchase.items.map((item) => ({
        variantId: item.variantId,
        productName:
          item.productName ||
          resolvedLabels[item.variantId]?.productName ||
          t("purchases.form.items.unresolvedVariant", { id: item.variantId.slice(0, 8) }),
        variantLabel: item.variantLabel || resolvedLabels[item.variantId]?.variantLabel || "—",
        sku: item.sku,
        qty: parseQty(item.qty),
        unitCost: parseMoney(item.unitCost) ?? 0,
      })),
    );
    setItemsInitialized(true);
  }, [isEdit, purchase, itemsInitialized, resolvedLabels, t]);

  function updateItemQty(variantId: string, qty: number) {
    setItems((prev) => prev.map((row) => (row.variantId === variantId ? { ...row, qty } : row)));
  }
  function updateItemUnitCost(variantId: string, unitCost: number) {
    setItems((prev) =>
      prev.map((row) => (row.variantId === variantId ? { ...row, unitCost } : row)),
    );
  }
  function removeItem(variantId: string) {
    setItems((prev) => prev.filter((row) => row.variantId !== variantId));
  }

  // The "add item" picker: search products by name/SKU, then pick one of
  // its variants.
  const [searchInput, setSearchInput] = useState("");
  const [debouncedSearch, setDebouncedSearch] = useState("");
  useEffect(() => {
    const timer = setTimeout(() => setDebouncedSearch(searchInput.trim()), SEARCH_DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [searchInput]);

  const { data: productResults } = useQuery({
    queryKey: ["purchase-item-products", debouncedSearch],
    queryFn: () => fetchProductsPage({ q: debouncedSearch }, null),
    enabled: debouncedSearch.length >= MIN_QUERY_LENGTH,
  });

  const [selectedProductId, setSelectedProductId] = useState<string | undefined>(undefined);
  const { data: productVariants } = useQuery({
    queryKey: ["purchase-item-variants", selectedProductId],
    queryFn: () => fetchVariants(selectedProductId as string),
    enabled: selectedProductId != null,
  });

  const [selectedVariantId, setSelectedVariantId] = useState<string | undefined>(undefined);
  const [newQty, setNewQty] = useState<number | null>(null);
  const [newUnitCost, setNewUnitCost] = useState<number | null>(null);

  function handleAddItem() {
    const product = productResults?.items.find((candidate) => candidate.id === selectedProductId);
    const variant = productVariants?.find((candidate) => candidate.id === selectedVariantId);
    if (!product || !variant || newQty == null || newQty <= 0) {
      return;
    }
    const label = variantLabel(variant, t);
    setResolvedLabels((prev) => ({
      ...prev,
      [variant.id]: { productName: product.name, variantLabel: label },
    }));
    setItems((prev) => {
      const nextRow: ItemRow = {
        variantId: variant.id,
        productName: product.name,
        variantLabel: label,
        sku: variant.sku,
        qty: newQty,
        unitCost: newUnitCost ?? 0,
      };
      const existingIndex = prev.findIndex((row) => row.variantId === variant.id);
      if (existingIndex >= 0) {
        const copy = [...prev];
        copy[existingIndex] = nextRow;
        return copy;
      }
      return [...prev, nextRow];
    });
    setSelectedProductId(undefined);
    setSelectedVariantId(undefined);
    setNewQty(null);
    setNewUnitCost(null);
    setSearchInput("");
  }

  const createMutation = useMutation({
    mutationFn: (body: PurchaseCreate) => createPurchase(body),
    onSuccess: async (created) => {
      await queryClient.invalidateQueries({ queryKey: ["purchases"] });
      notification.success({ message: t("purchases.saved") });
      await navigate({ to: "/purchases/$id", params: { id: created.id } });
    },
    onError: (error) => {
      if (!applyApiErrorToForm(form, error, t)) {
        notifyApiError(notification, error, t);
      }
    },
  });

  const editMutation = useMutation({
    mutationFn: (body: PurchasePatch) => updatePurchase(purchaseId as string, body),
    onSuccess: async (updated) => {
      queryClient.setQueryData(["purchase", purchaseId], updated);
      await queryClient.invalidateQueries({ queryKey: ["purchases"] });
      notification.success({ message: t("purchases.saved") });
    },
    onError: (error) => {
      if (!applyApiErrorToForm(form, error, t)) {
        notifyApiError(notification, error, t);
      }
    },
  });

  function buildItemsPayload(): PurchaseItemCreate[] {
    return items.map((row) => ({
      variantId: row.variantId,
      qty: formatQty(row.qty),
      unitCost: formatMoney(row.unitCost) as string,
    }));
  }

  function handleFinish(values: PurchaseMainFormValues) {
    // The contract requires at least one item (`minItems: 1`) — the Save
    // button is disabled for this case too, but a form can also submit via
    // Enter in a text field, bypassing that; guard here so no request with
    // an empty `items` array is ever sent.
    if (items.length === 0) {
      notification.error({ message: t("purchases.form.items.required") });
      return;
    }

    if (!isEdit) {
      const body: PurchaseCreate = {
        supplierId: values.supplierId as string,
        locationId: values.locationId as string,
        ...(values.supplierInvoiceNo?.trim()
          ? { supplierInvoiceNo: values.supplierInvoiceNo.trim() }
          : {}),
        ...(values.note?.trim() ? { note: values.note.trim() } : {}),
        items: buildItemsPayload(),
      };
      createMutation.mutate(body);
      return;
    }

    if (!purchase) {
      return;
    }

    const patch: PurchasePatch = {};
    if (values.supplierId && values.supplierId !== purchase.supplierId) {
      patch.supplierId = values.supplierId;
    }
    if (values.locationId && values.locationId !== purchase.locationId) {
      patch.locationId = values.locationId;
    }

    const normInvoiceNo = values.supplierInvoiceNo?.trim() || null;
    if (normInvoiceNo !== (purchase.supplierInvoiceNo ?? null)) {
      patch.supplierInvoiceNo = normInvoiceNo;
    }

    const normNote = values.note?.trim() || null;
    if (normNote !== (purchase.note ?? null)) {
      patch.note = normNote;
    }

    // Always sent in full on an edit save — the endpoint replaces the
    // whole list when `items` is provided (docs/05-API.md), and diffing
    // rows one by one against the loaded purchase would be fragile for no
    // benefit.
    patch.items = buildItemsPayload();

    editMutation.mutate(patch);
  }

  // Receive: the `Idempotency-Key` is generated once, when the dialog
  // opens, and reused for every retry from that same dialog session — only
  // reopening the dialog generates a new one (T6a spec).
  const [receiveOpen, setReceiveOpen] = useState(false);
  const [receiveKey, setReceiveKey] = useState<string | null>(null);

  function openReceiveDialog() {
    setReceiveKey(crypto.randomUUID());
    setReceiveOpen(true);
  }

  const receiveMutation = useMutation({
    mutationFn: () => receivePurchase(purchaseId as string, receiveKey as string),
    onSuccess: async (updated) => {
      queryClient.setQueryData(["purchase", purchaseId], updated);
      await queryClient.invalidateQueries({ queryKey: ["purchases"] });
      setReceiveOpen(false);
      notification.success({ message: t("purchases.receive.success") });
    },
    onError: (error) => {
      // The dialog stays open on error so a retry reuses the same key.
      notifyApiError(notification, error, t);
    },
  });

  const cancelMutation = useMutation({
    mutationFn: () => cancelPurchase(purchaseId as string),
    onSuccess: async (updated) => {
      queryClient.setQueryData(["purchase", purchaseId], updated);
      await queryClient.invalidateQueries({ queryKey: ["purchases"] });
      notification.success({ message: t("purchases.cancel.success") });
    },
    onError: (error) => {
      if (error instanceof ApiError && error.code === "STOCK_INSUFFICIENT") {
        const details = error.details as { variantId?: string; available?: string };
        const serverItem = purchase?.items.find((item) => item.variantId === details.variantId);
        const variant = serverItem?.productName ?? details.variantId?.slice(0, 8) ?? "—";
        notification.error({
          message: t("purchases.errors.stockInsufficient", {
            variant,
            available: details.available ?? "0",
          }),
        });
        return;
      }
      notifyApiError(notification, error, t);
    },
  });

  // "Purchases › P-000012" (create: "Purchases › New purchase"), plus a
  // back arrow to the list — both navigate to `purchasesRoute` (D-39).
  const breadcrumbLabel = isEdit
    ? (purchase?.number ?? t("purchases.editTitle"))
    : t("purchases.createTitle");
  const header = (
    <Space>
      <Button
        type="text"
        aria-label={t("common.back")}
        icon={<ArrowLeft size={16} />}
        onClick={() => navigate({ to: "/purchases" })}
      />
      <Breadcrumb
        items={[
          {
            title: t("purchases.title"),
            href: "/purchases",
            onClick: (event) => {
              event.preventDefault();
              navigate({ to: "/purchases" });
            },
          },
          { title: breadcrumbLabel },
        ]}
      />
    </Space>
  );

  if (isEdit && purchasePending) {
    return (
      <Card title={header}>
        <Skeleton active />
      </Card>
    );
  }

  const saving = createMutation.isPending || editMutation.isPending;

  const initialValues: Partial<PurchaseMainFormValues> =
    isEdit && purchase
      ? {
          supplierId: purchase.supplierId,
          locationId: purchase.locationId,
          supplierInvoiceNo: purchase.supplierInvoiceNo ?? undefined,
          note: purchase.note ?? undefined,
        }
      : {};

  const itemColumns: ColumnsType<ItemRow> = [
    { title: t("purchases.form.items.columns.product"), dataIndex: "productName" },
    {
      title: t("purchases.form.items.columns.variant"),
      key: "variantLabel",
      render: (_, row) => (
        <>
          <span>{row.variantLabel}</span>
          {row.sku && <Typography.Text type="secondary"> ({row.sku})</Typography.Text>}
        </>
      ),
    },
    {
      title: t("purchases.form.items.columns.qty"),
      key: "qty",
      render: (_, row) => (
        <InputNumber
          min={0.001}
          precision={3}
          value={row.qty}
          disabled={!editable}
          onChange={(value) => updateItemQty(row.variantId, value ?? 0)}
        />
      ),
    },
    {
      title: t("purchases.form.items.columns.unitCost"),
      key: "unitCost",
      render: (_, row) => (
        <InputNumber
          min={0}
          precision={2}
          value={row.unitCost}
          disabled={!editable}
          onChange={(value) => updateItemUnitCost(row.variantId, value ?? 0)}
        />
      ),
    },
    {
      title: t("purchases.form.items.columns.lineTotal"),
      key: "lineTotal",
      // Client-side display only — the server recomputes `totalCost`
      // authoritatively (hard rule 8); this value is never sent.
      render: (_, row) => (row.qty * row.unitCost).toFixed(2),
    },
    ...(editable
      ? ([
          {
            title: "",
            key: "actions",
            render: (_: unknown, row: ItemRow) => (
              <Button size="small" danger onClick={() => removeItem(row.variantId)}>
                {t("purchases.form.items.remove")}
              </Button>
            ),
          },
        ] satisfies ColumnsType<ItemRow>)
      : []),
  ];

  const receiveSummary = (purchase?.items ?? []).map((item) => ({
    variantId: item.variantId,
    productName: item.productName,
    variantLabel: item.variantLabel,
    qty: item.qty,
    unitCost: item.unitCost,
  }));

  return (
    <Card
      title={header}
      extra={
        <Space>
          {isEdit && purchase?.status === "draft" && (
            <Button onClick={openReceiveDialog}>{t("purchases.receive.action")}</Button>
          )}
          {isEdit && (purchase?.status === "draft" || purchase?.status === "received") && (
            <Popconfirm
              title={t("purchases.cancel.confirm")}
              onConfirm={() => cancelMutation.mutate()}
            >
              <Button danger loading={cancelMutation.isPending}>
                {t("purchases.cancel.action")}
              </Button>
            </Popconfirm>
          )}
          {editable && (
            <Button
              type="primary"
              loading={saving}
              disabled={items.length === 0}
              onClick={() => form.submit()}
            >
              {t("common.save")}
            </Button>
          )}
        </Space>
      }
    >
      <Form<PurchaseMainFormValues>
        form={form}
        layout="vertical"
        initialValues={initialValues}
        onFinish={handleFinish}
      >
        <FormGrid>
          <FormGrid.Item>
            <Form.Item
              name="supplierId"
              label={t("purchases.fields.supplier")}
              rules={[{ required: true }]}
            >
              <Select
                disabled={!editable}
                showSearch
                optionFilterProp="label"
                options={(suppliersPage?.items ?? []).map((supplier) => ({
                  value: supplier.id,
                  label: supplier.name,
                }))}
              />
            </Form.Item>
          </FormGrid.Item>
          <FormGrid.Item>
            <Form.Item
              name="locationId"
              label={t("purchases.fields.location")}
              rules={[{ required: true }]}
            >
              <Select
                disabled={!editable}
                options={(locationsPage?.items ?? []).map((location) => ({
                  value: location.id,
                  label: location.name,
                }))}
              />
            </Form.Item>
          </FormGrid.Item>
          <FormGrid.Item>
            <Form.Item name="supplierInvoiceNo" label={t("purchases.fields.supplierInvoiceNo")}>
              <Input disabled={!editable} />
            </Form.Item>
          </FormGrid.Item>
          <FormGrid.Item span="full">
            <Form.Item name="note" label={t("purchases.fields.note")}>
              <Input.TextArea rows={3} disabled={!editable} />
            </Form.Item>
          </FormGrid.Item>
        </FormGrid>
      </Form>

      <Card type="inner" title={t("purchases.form.items.title")} style={{ marginTop: 16 }}>
        {editable && (
          <Space style={{ marginBottom: 16 }} wrap align="start">
            <Select
              showSearch
              aria-label={t("purchases.form.items.productPlaceholder")}
              style={{ width: 240 }}
              placeholder={t("purchases.form.items.productPlaceholder")}
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
              aria-label={t("purchases.form.items.variantPlaceholder")}
              style={{ width: 220 }}
              placeholder={t("purchases.form.items.variantPlaceholder")}
              disabled={!selectedProductId}
              value={selectedVariantId}
              onChange={(value: string) => setSelectedVariantId(value)}
              options={(productVariants ?? []).map((variant) => ({
                value: variant.id,
                label: variantLabel(variant, t),
              }))}
            />
            <InputNumber
              aria-label={t("purchases.form.items.qty")}
              min={0.001}
              precision={3}
              placeholder={t("purchases.form.items.qty")}
              value={newQty}
              onChange={setNewQty}
            />
            <InputNumber
              aria-label={t("purchases.form.items.unitCost")}
              min={0}
              precision={2}
              placeholder={t("purchases.form.items.unitCost")}
              value={newUnitCost}
              onChange={setNewUnitCost}
            />
            <Button
              onClick={handleAddItem}
              disabled={!selectedVariantId || newQty == null || newQty <= 0}
            >
              {t("purchases.form.items.add")}
            </Button>
          </Space>
        )}

        <Table<ItemRow>
          rowKey="variantId"
          dataSource={items}
          pagination={false}
          locale={{ emptyText: t("purchases.form.items.empty") }}
          columns={itemColumns}
        />
        {editable && items.length === 0 && (
          <p style={{ color: "#ff4d4f", marginTop: 12, marginBottom: 0 }}>
            {t("purchases.form.items.required")}
          </p>
        )}
      </Card>

      <Modal
        title={t("purchases.receive.title")}
        open={receiveOpen}
        onCancel={() => setReceiveOpen(false)}
        onOk={() => receiveMutation.mutate()}
        confirmLoading={receiveMutation.isPending}
      >
        <p>{t("purchases.receive.confirm")}</p>
        <Table
          rowKey="variantId"
          dataSource={receiveSummary}
          pagination={false}
          size="small"
          columns={[
            { title: t("purchases.form.items.columns.product"), dataIndex: "productName" },
            { title: t("purchases.form.items.columns.variant"), dataIndex: "variantLabel" },
            { title: t("purchases.form.items.columns.qty"), dataIndex: "qty" },
            { title: t("purchases.form.items.columns.unitCost"), dataIndex: "unitCost" },
          ]}
        />
        {purchase && (
          <p style={{ marginTop: 12 }}>
            {t("purchases.receive.total", { total: purchase.totalCost })}
          </p>
        )}
      </Modal>
    </Card>
  );
}
