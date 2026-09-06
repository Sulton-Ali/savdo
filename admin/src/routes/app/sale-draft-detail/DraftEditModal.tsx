import { useMutation, useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { App, Form, Input, InputNumber, Modal, Select } from "antd";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import { fetchCustomer, fetchCustomersPage } from "../../../customers/api";
import { ApiError, applyApiErrorToForm, notifyApiError } from "../../../lib/errors";
import { formatMoney } from "../../../lib/money";
import type { DiscountType } from "../../../sales/api";
import { type SaleDraft, type SaleDraftPatch, updateSaleDraft } from "../../../sales/draftsApi";

const DISCOUNT_TYPES: DiscountType[] = ["percent", "fixed"];
const MIN_QUERY_LENGTH = 2;
const SEARCH_DEBOUNCE_MS = 300;

/** A customer option kept regardless of the live search results
 * (`labelInValue`) — same shape `SalesListPage`'s customer filter uses, so
 * the draft's already-attached customer still renders a name even before
 * any search has run. */
interface CustomerOption {
  value: string;
  label: string;
}

interface EditFormValues {
  customer?: CustomerOption;
  discountType?: DiscountType;
  discountValue?: number;
  note?: string;
}

/**
 * Edit a draft's note, discount and customer (creator or manager+, D-89 —
 * the button that opens this modal is already gated by
 * `canManageDraft`). Line items are out of scope for this page (the task
 * this shipped under does not build line editing); the modal only ever
 * sends `note`/`discountType`/`discountValue`/`customerId` — never `items`.
 * Clearing the discount (removing its type) sends an explicit `discountType:
 * null`, which clears the whole type/value pair per D-35's pairing rule for
 * `SaleDraftPatch`.
 */
export function DraftEditModal({
  draft,
  open,
  onClose,
  onUpdated,
}: {
  draft: SaleDraft;
  open: boolean;
  onClose: () => void;
  onUpdated: (draft: SaleDraft) => void;
}) {
  const { t } = useTranslation();
  const { notification } = App.useApp();
  const navigate = useNavigate();
  const [form] = Form.useForm<EditFormValues>();
  const [discountType, setDiscountType] = useState<DiscountType | undefined>(draft.discount?.type);

  const { data: currentCustomer } = useQuery({
    queryKey: ["customer", draft.customerId],
    queryFn: () => fetchCustomer(draft.customerId as string),
    enabled: draft.customerId != null && open,
  });

  const [customerSearch, setCustomerSearch] = useState("");
  const [debouncedCustomerSearch, setDebouncedCustomerSearch] = useState("");
  useEffect(() => {
    const timer = setTimeout(
      () => setDebouncedCustomerSearch(customerSearch.trim()),
      SEARCH_DEBOUNCE_MS,
    );
    return () => clearTimeout(timer);
  }, [customerSearch]);
  const { data: customerResults } = useQuery({
    queryKey: ["draft-edit-customers", debouncedCustomerSearch],
    queryFn: () => fetchCustomersPage({ q: debouncedCustomerSearch }, null),
    enabled: open && debouncedCustomerSearch.length >= MIN_QUERY_LENGTH,
  });

  useEffect(() => {
    if (!open) {
      return;
    }
    form.setFieldsValue({
      customer:
        draft.customerId != null
          ? { value: draft.customerId, label: currentCustomer?.fullName ?? "…" }
          : undefined,
      discountType: draft.discount?.type,
      discountValue: draft.discount ? Number(draft.discount.value) : undefined,
      note: draft.note ?? undefined,
    });
    setDiscountType(draft.discount?.type);
    setCustomerSearch("");
  }, [open, draft, currentCustomer, form]);

  const customerOptions: CustomerOption[] = [
    ...(draft.customerId != null && currentCustomer
      ? [{ value: draft.customerId, label: currentCustomer.fullName }]
      : []),
    ...(customerResults?.items ?? [])
      .filter((customer) => customer.id !== draft.customerId)
      .map((customer) => ({ value: customer.id, label: customer.fullName })),
  ];

  const mutation = useMutation({
    mutationFn: (values: EditFormValues) => {
      const patch: SaleDraftPatch = {};

      const normCustomerId = values.customer?.value ?? null;
      if (normCustomerId !== (draft.customerId ?? null)) {
        patch.customerId = normCustomerId;
      }

      const hasDiscount =
        values.discountType != null && values.discountValue != null && values.discountValue > 0;
      const originalType = draft.discount?.type ?? null;
      const originalValue = draft.discount?.value ?? null;
      if (hasDiscount) {
        const normValue = formatMoney(values.discountValue) as string;
        if (values.discountType !== originalType || normValue !== originalValue) {
          patch.discountType = values.discountType as DiscountType;
          patch.discountValue = normValue;
        }
      } else if (draft.discount != null) {
        // Clearing either half of the type/value pair clears the whole
        // discount (D-35) — sending `discountType: null` is enough.
        patch.discountType = null;
      }

      const normNote = values.note?.trim() || null;
      if (normNote !== (draft.note ?? null)) {
        patch.note = normNote;
      }

      return updateSaleDraft(draft.id, patch);
    },
    onSuccess: (updated) => {
      onUpdated(updated);
      onClose();
      notification.success({ message: t("sales.drafts.edit.success") });
    },
    onError: (error) => {
      if (error instanceof ApiError && error.code === "NOT_FOUND") {
        onClose();
        notification.info({ message: t("sales.drafts.errors.notFound") });
        void navigate({ to: "/sales/drafts" });
        return;
      }
      if (error instanceof ApiError && error.code === "DISCOUNT_EXCEEDS_SUBTOTAL") {
        notification.error({ message: t("sales.errors.discountExceedsSubtotal") });
        return;
      }
      if (!applyApiErrorToForm(form, error, t)) {
        notifyApiError(notification, error, t);
      }
    },
  });

  return (
    <Modal
      title={t("sales.drafts.edit.title")}
      open={open}
      onCancel={onClose}
      onOk={() => form.submit()}
      okText={t("sales.drafts.edit.submit")}
      confirmLoading={mutation.isPending}
    >
      <Form<EditFormValues>
        form={form}
        layout="vertical"
        onValuesChange={(changed) => {
          if ("discountType" in changed) {
            setDiscountType(changed.discountType);
            if (!changed.discountType) {
              form.setFieldValue("discountValue", undefined);
            }
          }
        }}
        onFinish={(values) => mutation.mutate(values)}
      >
        <Form.Item name="customer" label={t("sales.fields.customer")}>
          <Select<CustomerOption | undefined>
            aria-label={t("sales.fields.customer")}
            allowClear
            showSearch
            labelInValue
            filterOption={false}
            placeholder={t("sales.customerPlaceholder")}
            onSearch={setCustomerSearch}
            options={customerOptions}
          />
        </Form.Item>
        <Form.Item name="discountType" label={t("sales.fields.discountType")}>
          <Select
            aria-label={t("sales.fields.discountType")}
            allowClear
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
        <Form.Item name="note" label={t("sales.fields.note")}>
          <Input.TextArea rows={2} />
        </Form.Item>
      </Form>
    </Modal>
  );
}
