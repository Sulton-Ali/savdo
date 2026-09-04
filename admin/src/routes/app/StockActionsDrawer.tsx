import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { App, Button, Drawer, Form, Input, InputNumber, Select } from "antd";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import { ApiError, applyApiErrorToForm, notifyApiError } from "../../lib/errors";
import {
  type AdjustmentReason,
  createStockAdjustment,
  createStockTransfer,
  fetchAllLocations,
  type StockAdjustmentCreate,
  type StockTransferCreate,
} from "../../stock/api";
import { StockVariantPicker, type StockVariantValue } from "./StockVariantPicker";

const ADJUSTMENT_REASONS: AdjustmentReason[] = [
  "count_correction",
  "damaged",
  "lost",
  "found",
  "other",
];

const EMPTY_VARIANT: StockVariantValue = { productId: null, variantId: null };

function requireVariant(t: (key: string) => string) {
  return {
    validator: (_: unknown, value: StockVariantValue | undefined) =>
      value?.variantId
        ? Promise.resolve()
        : Promise.reject(new Error(t("stock.errors.variantRequired"))),
  };
}

/** Details shape of a `409 STOCK_INSUFFICIENT` error (`docs/05-API.md` §
 * Conventions): `{ variantId, locationId, available }`. */
interface StockInsufficientDetails {
  available?: string;
}

function invalidateStock(queryClient: ReturnType<typeof useQueryClient>) {
  return Promise.all([
    queryClient.invalidateQueries({ queryKey: ["stock", "levels"] }),
    queryClient.invalidateQueries({ queryKey: ["stock", "movements"] }),
    queryClient.invalidateQueries({ queryKey: ["stock", "low"] }),
  ]);
}

interface AdjustmentFormValues {
  variant: StockVariantValue;
  locationId: string;
  qty: number;
  reason: AdjustmentReason;
  note?: string;
}

/**
 * Manual stock adjustment (D-46, manager+): a signed qty against one
 * variant/location — positive "found" stock (increases the level),
 * negative "lost/damaged" stock (decreases it). `Idempotency-Key` is
 * generated once when the drawer opens and reused on every retry of the
 * same submission (`docs/05-API.md` § Conventions), so a duplicate click or
 * a retried network failure never double-applies the adjustment.
 */
export function StockAdjustmentDrawer({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { t } = useTranslation();
  const { notification } = App.useApp();
  const queryClient = useQueryClient();
  const [form] = Form.useForm<AdjustmentFormValues>();
  const [idempotencyKey, setIdempotencyKey] = useState(() => crypto.randomUUID());

  useEffect(() => {
    if (open) {
      setIdempotencyKey(crypto.randomUUID());
      form.resetFields();
    }
  }, [open, form]);

  const { data: locations } = useQuery({
    queryKey: ["locations", "all"],
    queryFn: fetchAllLocations,
  });

  const mutation = useMutation({
    mutationFn: (values: AdjustmentFormValues) => {
      const body: StockAdjustmentCreate = {
        variantId: values.variant.variantId as string,
        locationId: values.locationId,
        qty: values.qty.toFixed(3),
        reason: values.reason,
        ...(values.note?.trim() ? { note: values.note.trim() } : {}),
      };
      return createStockAdjustment(body, idempotencyKey);
    },
    onSuccess: async () => {
      await invalidateStock(queryClient);
      onClose();
    },
    onError: (error) => {
      if (error instanceof ApiError && error.code === "STOCK_INSUFFICIENT") {
        const { available } = error.details as StockInsufficientDetails;
        form.setFields([
          {
            name: "qty",
            errors: [t("stock.errors.insufficient", { available: available ?? "0" })],
          },
        ]);
        return;
      }
      if (!applyApiErrorToForm(form, error, t)) {
        notifyApiError(notification, error, t);
      }
    },
  });

  return (
    <Drawer
      title={t("stock.actions.adjust")}
      open={open}
      onClose={onClose}
      extra={
        <Button type="primary" loading={mutation.isPending} onClick={() => form.submit()}>
          {t("common.save")}
        </Button>
      }
    >
      <Form<AdjustmentFormValues>
        form={form}
        layout="vertical"
        initialValues={{ variant: EMPTY_VARIANT }}
        onFinish={(values) => mutation.mutate(values)}
      >
        <Form.Item name="variant" label={t("stock.fields.variant")} rules={[requireVariant(t)]}>
          <StockVariantPicker />
        </Form.Item>
        <Form.Item
          name="locationId"
          label={t("stock.fields.location")}
          rules={[{ required: true, message: t("stock.errors.locationRequired") }]}
        >
          <Select
            options={(locations ?? []).map((location) => ({
              value: location.id,
              label: location.name,
            }))}
          />
        </Form.Item>
        <Form.Item
          name="qty"
          label={t("stock.fields.adjustmentQty")}
          extra={t("stock.fields.adjustmentQtyHint")}
          rules={[{ required: true, message: t("stock.errors.qtyRequired") }]}
        >
          <InputNumber precision={3} style={{ width: "100%" }} />
        </Form.Item>
        <Form.Item
          name="reason"
          label={t("stock.fields.reason")}
          rules={[{ required: true, message: t("stock.errors.reasonRequired") }]}
        >
          <Select
            options={ADJUSTMENT_REASONS.map((reason) => ({
              value: reason,
              label: t(`stock.adjustmentReasons.${reason}`),
            }))}
          />
        </Form.Item>
        <Form.Item name="note" label={t("stock.fields.note")}>
          <Input.TextArea rows={3} />
        </Form.Item>
      </Form>
    </Drawer>
  );
}

interface TransferFormValues {
  variant: StockVariantValue;
  fromLocationId: string;
  toLocationId: string;
  qty: number;
}

/** Moves stock between two locations (manager+): writes a `transfer_out`
 * movement at `fromLocationId` and a `transfer_in` movement at
 * `toLocationId` server-side (`stock.Service.Move`, ADR-006). The same
 * location on both sides is rejected client-side before submit, and mapped
 * from the server's `409 SAME_LOCATION` onto `toLocationId` if it slips
 * through (e.g. a race with another transfer). No `Idempotency-Key` — the
 * contract's `createStockTransfer` operation defines none, unlike
 * adjustments. */
export function StockTransferDrawer({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { t } = useTranslation();
  const { notification } = App.useApp();
  const queryClient = useQueryClient();
  const [form] = Form.useForm<TransferFormValues>();

  useEffect(() => {
    if (open) {
      form.resetFields();
    }
  }, [open, form]);

  const { data: locations } = useQuery({
    queryKey: ["locations", "all"],
    queryFn: fetchAllLocations,
  });
  const locationOptions = (locations ?? []).map((location) => ({
    value: location.id,
    label: location.name,
  }));

  const mutation = useMutation({
    mutationFn: (values: TransferFormValues) => {
      const body: StockTransferCreate = {
        variantId: values.variant.variantId as string,
        fromLocationId: values.fromLocationId,
        toLocationId: values.toLocationId,
        qty: values.qty.toFixed(3),
      };
      return createStockTransfer(body);
    },
    onSuccess: async () => {
      await invalidateStock(queryClient);
      onClose();
    },
    onError: (error) => {
      if (error instanceof ApiError && error.code === "STOCK_INSUFFICIENT") {
        const { available } = error.details as StockInsufficientDetails;
        form.setFields([
          {
            name: "qty",
            errors: [t("stock.errors.insufficient", { available: available ?? "0" })],
          },
        ]);
        return;
      }
      if (error instanceof ApiError && error.code === "SAME_LOCATION") {
        form.setFields([{ name: "toLocationId", errors: [t("stock.errors.sameLocation")] }]);
        return;
      }
      if (!applyApiErrorToForm(form, error, t)) {
        notifyApiError(notification, error, t);
      }
    },
  });

  return (
    <Drawer
      title={t("stock.actions.transfer")}
      open={open}
      onClose={onClose}
      extra={
        <Button type="primary" loading={mutation.isPending} onClick={() => form.submit()}>
          {t("common.save")}
        </Button>
      }
    >
      <Form<TransferFormValues>
        form={form}
        layout="vertical"
        initialValues={{ variant: EMPTY_VARIANT }}
        onFinish={(values) => mutation.mutate(values)}
      >
        <Form.Item name="variant" label={t("stock.fields.variant")} rules={[requireVariant(t)]}>
          <StockVariantPicker />
        </Form.Item>
        <Form.Item
          name="fromLocationId"
          label={t("stock.fields.fromLocation")}
          rules={[{ required: true, message: t("stock.errors.locationRequired") }]}
        >
          <Select options={locationOptions} />
        </Form.Item>
        <Form.Item
          name="toLocationId"
          label={t("stock.fields.toLocation")}
          dependencies={["fromLocationId"]}
          rules={[
            { required: true, message: t("stock.errors.locationRequired") },
            {
              validator: (_, value: string | undefined) =>
                value && value === form.getFieldValue("fromLocationId")
                  ? Promise.reject(new Error(t("stock.errors.sameLocation")))
                  : Promise.resolve(),
            },
          ]}
        >
          <Select options={locationOptions} />
        </Form.Item>
        <Form.Item
          name="qty"
          label={t("stock.fields.transferQty")}
          rules={[{ required: true, message: t("stock.errors.qtyRequired") }]}
        >
          <InputNumber min={0.001} precision={3} style={{ width: "100%" }} />
        </Form.Item>
      </Form>
    </Drawer>
  );
}
