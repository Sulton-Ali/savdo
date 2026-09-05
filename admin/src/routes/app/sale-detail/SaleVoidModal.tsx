import { useMutation } from "@tanstack/react-query";
import { App, Form, Input, Modal } from "antd";
import { useEffect } from "react";
import { useTranslation } from "react-i18next";

import { ApiError, notifyApiError } from "../../../lib/errors";
import { type Sale, voidSale } from "../../../sales/api";

interface VoidFormValues {
  reason?: string;
}

/** Machine-readable code → i18n key for the errors `POST /sales/{id}/void`
 * defines (`docs/05-API.md` § Conventions, ADR-013): anything else falls
 * back to `notifyApiError`'s generic message. */
const VOID_ERROR_KEYS: Partial<Record<string, string>> = {
  SALE_VOID_WINDOW_CLOSED: "sales.errors.voidWindowClosed",
  SALE_HAS_RETURNS: "sales.errors.saleHasReturns",
  SALE_ALREADY_VOIDED: "sales.errors.saleAlreadyVoided",
};

/**
 * Void a completed sale (manager+, `sales.void`). `SaleDetailPage` only
 * renders the button that opens this modal when `kind: sale`,
 * `status: completed` and `hasReturns: false` hold, but the server remains
 * the enforcement point (ADR-010) — a race is still reported through the
 * same mapped messages.
 */
export function SaleVoidModal({
  sale,
  open,
  onClose,
  onVoided,
}: {
  sale: Sale;
  open: boolean;
  onClose: () => void;
  onVoided: (sale: Sale) => void;
}) {
  const { t } = useTranslation();
  const { notification } = App.useApp();
  const [form] = Form.useForm<VoidFormValues>();

  useEffect(() => {
    if (open) {
      form.resetFields();
    }
  }, [open, form]);

  const mutation = useMutation({
    mutationFn: (values: VoidFormValues) =>
      voidSale(sale.id, values.reason?.trim() ? { reason: values.reason.trim() } : {}),
    onSuccess: (voided) => {
      onVoided(voided);
      onClose();
      notification.success({ message: t("sales.void.success") });
    },
    onError: (error) => {
      const key = error instanceof ApiError ? VOID_ERROR_KEYS[error.code] : undefined;
      if (key) {
        notification.error({ message: t(key) });
        return;
      }
      notifyApiError(notification, error, t);
    },
  });

  return (
    <Modal
      title={t("sales.void.title")}
      open={open}
      onCancel={onClose}
      onOk={() => form.submit()}
      confirmLoading={mutation.isPending}
    >
      <Form<VoidFormValues>
        form={form}
        layout="vertical"
        onFinish={(values) => mutation.mutate(values)}
      >
        <Form.Item name="reason" label={t("sales.void.reason")}>
          <Input.TextArea rows={3} />
        </Form.Item>
      </Form>
    </Modal>
  );
}
