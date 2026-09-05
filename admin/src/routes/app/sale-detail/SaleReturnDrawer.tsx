import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { App, Button, Drawer, Input, InputNumber, Space, Table, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import { ApiError, notifyApiError } from "../../../lib/errors";
import {
  createSaleReturn,
  type Sale,
  type SaleReturnCreate,
  type SaleReturnItemCreate,
} from "../../../sales/api";

/** Machine-readable code → i18n key for the errors `POST /sales/{id}/return`
 * defines (`docs/05-API.md` § Conventions, ADR-013): anything else falls
 * back to `notifyApiError`'s generic message. `RETURN_EXCEEDS_SOLD` also
 * carries `details.saleItemId`, but the button that opens this drawer
 * already caps every line client-side (D-58), so a page-level notification
 * is enough for the rare server-side race this reports. */
const RETURN_ERROR_KEYS: Partial<Record<string, string>> = {
  RETURN_EXCEEDS_SOLD: "sales.errors.returnExceedsSold",
  SALE_ALREADY_VOIDED: "sales.errors.saleAlreadyVoided",
  IDEMPOTENCY_KEY_REUSED: "sales.errors.returnIdempotencyKeyReused",
};

/** Counts the decimal places of a `Decimal` wire string (`"2.000"` → 3,
 * `"5"` → 0) — quantities carry the scale of their own line rather than a
 * fixed constant, so the return-qty input's `step`/`precision` follow suit. */
function decimalPlaces(value: string): number {
  const dotIndex = value.indexOf(".");
  return dotIndex === -1 ? 0 : value.length - dotIndex - 1;
}

/** Sold minus already returned, computed by scaling both to integers first
 * (`NUMERIC(12,3)`, hard rule 3) rather than subtracting the parsed floats
 * directly — the same three-decimal scale every quantity in this app uses,
 * so no float rounding artifact can push the cap a cent past what the
 * server would actually accept (D-58). */
function maxReturnable(qty: string, returnedQty: string): number {
  const scale = 1000;
  const soldUnits = Math.round(Number(qty) * scale);
  const returnedUnits = Math.round(Number(returnedQty) * scale);
  return Math.max(0, soldUnits - returnedUnits) / scale;
}

interface ReturnRow {
  saleItemId: string;
  productName: string;
  variantLabel: string;
  qty: string;
  returnedQty: string;
  max: number;
  precision: number;
}

/**
 * Record a partial or full return against a completed sale (manager+,
 * `sales.void` — reused for return per the task, the server is the real
 * enforcement point). Per line the return qty is capped at sold minus
 * already returned (D-58). The `Idempotency-Key` is generated once when the
 * drawer opens and again every time the staged body (quantities or note)
 * changes, so a retry of the exact same submission reuses it (safe replay)
 * while an edited body never collides with a previous attempt's key on the
 * server (`docs/05-API.md` § Conventions).
 */
export function SaleReturnDrawer({
  sale,
  open,
  onClose,
}: {
  sale: Sale;
  open: boolean;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const { notification } = App.useApp();
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  const [quantities, setQuantities] = useState<Record<string, number>>({});
  const [note, setNote] = useState("");
  const [idempotencyKey, setIdempotencyKey] = useState<string | null>(null);

  useEffect(() => {
    if (open) {
      setQuantities({});
      setNote("");
    }
  }, [open]);

  // A deliberate re-trigger signal, not read inside the effect body below —
  // it changes whenever the staged return body changes, so a new key is
  // minted for every distinct submission while an unchanged retry reuses
  // the same one (docs/05-API.md § Conventions).
  const bodySignature = JSON.stringify({ quantities, note });
  // biome-ignore lint/correctness/useExhaustiveDependencies: see comment above
  useEffect(() => {
    if (open) {
      setIdempotencyKey(crypto.randomUUID());
    }
  }, [open, bodySignature]);

  const rows: ReturnRow[] = sale.items.map((item) => ({
    saleItemId: item.id,
    productName: item.productName,
    variantLabel: item.variantLabel,
    qty: item.qty,
    returnedQty: item.returnedQty,
    max: maxReturnable(item.qty, item.returnedQty),
    precision: decimalPlaces(item.qty),
  }));
  const rowsById = new Map(rows.map((row) => [row.saleItemId, row]));

  const mutation = useMutation({
    mutationFn: () => {
      const items: SaleReturnItemCreate[] = Object.entries(quantities)
        .filter(([, qty]) => qty > 0)
        .map(([saleItemId, qty]) => {
          const precision = rowsById.get(saleItemId)?.precision ?? 3;
          return { saleItemId, qty: qty.toFixed(precision) };
        });
      const body: SaleReturnCreate = {
        items,
        ...(note.trim() ? { note: note.trim() } : {}),
      };
      return createSaleReturn(sale.id, body, idempotencyKey as string);
    },
    onSuccess: async (created) => {
      await queryClient.invalidateQueries({ queryKey: ["sale", sale.id] });
      onClose();
      notification.success({ message: t("sales.return.success") });
      await navigate({ to: "/sales/$id", params: { id: created.id } });
    },
    onError: (error) => {
      const key = error instanceof ApiError ? RETURN_ERROR_KEYS[error.code] : undefined;
      if (key) {
        notification.error({ message: t(key) });
        return;
      }
      notifyApiError(notification, error, t);
    },
  });

  const hasAnyQty = Object.values(quantities).some((qty) => qty > 0);

  const columns: ColumnsType<ReturnRow> = [
    { title: t("sales.detail.columns.product"), dataIndex: "productName" },
    { title: t("sales.detail.columns.variant"), dataIndex: "variantLabel" },
    { title: t("sales.return.columns.sold"), dataIndex: "qty" },
    { title: t("sales.return.columns.returned"), dataIndex: "returnedQty" },
    {
      title: t("sales.return.columns.qty"),
      key: "returnQty",
      render: (_, row) => (
        <InputNumber
          aria-label={`${t("sales.return.columns.qty")} ${row.productName}`}
          min={0}
          max={row.max}
          step={row.precision === 0 ? 1 : 1 / 10 ** row.precision}
          precision={row.precision}
          disabled={row.max <= 0}
          value={quantities[row.saleItemId] ?? 0}
          onChange={(value) => {
            const capped = Math.min(Math.max(value ?? 0, 0), row.max);
            setQuantities((prev) => ({ ...prev, [row.saleItemId]: capped }));
          }}
        />
      ),
    },
  ];

  return (
    <Drawer
      title={t("sales.return.title")}
      open={open}
      onClose={onClose}
      width={640}
      extra={
        <Button
          type="primary"
          loading={mutation.isPending}
          disabled={!hasAnyQty}
          onClick={() => mutation.mutate()}
        >
          {t("sales.return.submit")}
        </Button>
      }
    >
      <Space direction="vertical" style={{ width: "100%" }} size="middle">
        <Table<ReturnRow>
          rowKey="saleItemId"
          columns={columns}
          dataSource={rows}
          pagination={false}
        />
        <div>
          <Typography.Text>{t("sales.return.note")}</Typography.Text>
          <Input.TextArea
            aria-label={t("sales.return.note")}
            rows={3}
            value={note}
            onChange={(event) => setNote(event.target.value)}
          />
        </div>
      </Space>
    </Drawer>
  );
}
