import { useMutation } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { App, Form, Modal, Select } from "antd";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import { ApiError, notifyApiError } from "../../../lib/errors";
import type { PaymentMethod } from "../../../sales/api";
import { completeSaleDraft, type SaleDraft } from "../../../sales/draftsApi";
import { findUnavailableLineField } from "../../../sales/draftsHelpers";

const PAYMENT_METHODS: PaymentMethod[] = ["cash", "card", "transfer"];

interface PayFormValues {
  paymentMethod: PaymentMethod;
}

/** Machine-readable code → i18n key this modal maps directly (no per-field
 * detail needed), mirroring `SaleVoidModal`/`SaleReturnDrawer`'s
 * `Partial<Record<ErrorCode, string>>` pattern (`docs/05-API.md` §
 * Conventions, ADR-013). `VALIDATION_FAILED` (unavailable line) and
 * `STOCK_INSUFFICIENT` (needs `details.available`) are handled separately
 * below since both need data out of `details`. */
const COMPLETE_ERROR_KEYS: Partial<Record<string, string>> = {
  DISCOUNT_EXCEEDS_SUBTOTAL: "sales.errors.discountExceedsSubtotal",
};

/**
 * Complete (Pay) a draft — any staff who may create a sale, regardless of
 * the draft's own creator (D-96): the button that opens this modal has no
 * permission gate beyond being logged in as staff. Requests
 * `POST /sales/drafts/{id}/complete` with a fresh `Idempotency-Key` per
 * distinct payment method choice, kept stable across a retry of the same
 * choice and re-minted on an explicit `IDEMPOTENCY_KEY_REUSED` (D-97,
 * mirroring `QuickSalePage`'s `POST /sales` key handling). On success,
 * navigates to the newly created sale's detail page.
 */
export function DraftPayModal({
  draft,
  open,
  onClose,
}: {
  draft: SaleDraft;
  open: boolean;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const { notification } = App.useApp();
  const navigate = useNavigate();
  const [form] = Form.useForm<PayFormValues>();

  const [paymentMethod, setPaymentMethod] = useState<PaymentMethod>("cash");
  const [idempotencyKey, setIdempotencyKey] = useState(() => crypto.randomUUID());
  const lastKeyedMethod = useRef(paymentMethod);

  useEffect(() => {
    if (open) {
      form.setFieldsValue({ paymentMethod: "cash" });
      setPaymentMethod("cash");
      lastKeyedMethod.current = "cash";
      setIdempotencyKey(crypto.randomUUID());
    }
  }, [open, form]);

  useEffect(() => {
    if (lastKeyedMethod.current === paymentMethod) {
      return;
    }
    lastKeyedMethod.current = paymentMethod;
    setIdempotencyKey(crypto.randomUUID());
  }, [paymentMethod]);

  const mutation = useMutation({
    mutationFn: () => completeSaleDraft(draft.id, { paymentMethod }, idempotencyKey),
    onSuccess: async (sale) => {
      onClose();
      notification.success({ message: t("sales.drafts.pay.success") });
      await navigate({ to: "/sales/$id", params: { id: sale.id } });
    },
    onError: (error) => {
      if (!(error instanceof ApiError)) {
        notifyApiError(notification, error, t);
        return;
      }
      if (error.code === "VALIDATION_FAILED") {
        const details = error.details as { fields?: Record<string, string> };
        const match = findUnavailableLineField(details.fields);
        if (match) {
          const item = draft.items[match.index];
          const line = item ? `${item.productName} — ${item.variantLabel}` : `#${match.index + 1}`;
          notification.error({ message: t("sales.drafts.errors.lineUnavailable", { line }) });
          return;
        }
      }
      if (error.code === "STOCK_INSUFFICIENT") {
        const details = error.details as { available?: string };
        notification.error({
          message: t("sales.errors.stockInsufficient", { available: details.available ?? "0" }),
        });
        return;
      }
      if (error.code === "IDEMPOTENCY_KEY_REUSED") {
        notification.error({ message: t("sales.errors.idempotencyKeyReused") });
        setIdempotencyKey(crypto.randomUUID());
        return;
      }
      if (error.code === "NOT_FOUND") {
        onClose();
        notification.info({ message: t("sales.drafts.errors.notFound") });
        void navigate({ to: "/sales/drafts" });
        return;
      }
      const key = COMPLETE_ERROR_KEYS[error.code];
      if (key) {
        notification.error({ message: t(key) });
        return;
      }
      notifyApiError(notification, error, t);
    },
  });

  return (
    <Modal
      title={t("sales.drafts.pay.title")}
      open={open}
      onCancel={onClose}
      onOk={() => mutation.mutate()}
      okText={t("sales.drafts.pay.submit")}
      confirmLoading={mutation.isPending}
    >
      <Form<PayFormValues>
        form={form}
        layout="vertical"
        initialValues={{ paymentMethod: "cash" }}
        onValuesChange={(_changed, allValues) => setPaymentMethod(allValues.paymentMethod)}
      >
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
      </Form>
    </Modal>
  );
}
