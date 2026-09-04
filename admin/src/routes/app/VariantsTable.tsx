import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  App,
  Button,
  Drawer,
  Form,
  type FormInstance,
  Input,
  InputNumber,
  Popconfirm,
  Switch,
  Table,
} from "antd";
import type { ColumnsType } from "antd/es/table";
import { forwardRef, useImperativeHandle, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import {
  type AttributeDefinition,
  deleteVariant,
  updateVariant,
  type Variant,
  type VariantPatch,
} from "../../catalog/api";
import { buildVariantPatch, type VariantEditFormValues } from "../../catalog/variants";
import { ApiError, notifyApiError } from "../../lib/errors";
import { parseMoney } from "../../lib/money";

const VariantEditForm = forwardRef<
  FormInstance<VariantEditFormValues>,
  {
    variant: Variant;
    attributeDefinitions: AttributeDefinition[];
    onFinish: (values: VariantEditFormValues) => void;
  }
>(function VariantEditForm({ variant, attributeDefinitions, onFinish }, ref) {
  const { t } = useTranslation();
  const [form] = Form.useForm<VariantEditFormValues>();
  useImperativeHandle(ref, () => form, [form]);

  const showCost = variant.costOverride !== undefined;

  const initialValues: VariantEditFormValues = {
    sku: variant.sku ?? undefined,
    barcode: variant.barcode ?? undefined,
    priceOverride: parseMoney(variant.priceOverride),
    costOverride: parseMoney(variant.costOverride),
    isActive: variant.isActive,
    attributes: { ...variant.attributes },
  };

  return (
    <Form<VariantEditFormValues>
      form={form}
      layout="vertical"
      initialValues={initialValues}
      onFinish={onFinish}
    >
      {attributeDefinitions.map((def) => (
        <Form.Item key={def.id} name={["attributes", def.code]} label={def.name}>
          <Input maxLength={64} />
        </Form.Item>
      ))}
      <Form.Item name="sku" label={t("catalog.variants.fields.sku")}>
        <Input />
      </Form.Item>
      <Form.Item name="barcode" label={t("catalog.variants.fields.barcode")}>
        <Input />
      </Form.Item>
      <Form.Item name="priceOverride" label={t("catalog.variants.fields.priceOverride")}>
        <InputNumber min={0} precision={2} style={{ width: "100%" }} />
      </Form.Item>
      {showCost && (
        <Form.Item name="costOverride" label={t("catalog.variants.fields.costOverride")}>
          <InputNumber min={0} precision={2} style={{ width: "100%" }} />
        </Form.Item>
      )}
      <Form.Item
        name="isActive"
        label={t("catalog.variants.fields.isActive")}
        valuePropName="checked"
      >
        <Switch />
      </Form.Item>
    </Form>
  );
});

/**
 * The variants table (T6b spec): one dynamic column per attribute
 * definition, SKU/barcode/overrides, an active switch, an edit drawer that
 * `PATCH`es only what changed (nullable clears, D-35), and delete with a
 * `400 fields.variantId: invalid` (the product's only variant) mapped to a
 * notification instead of a field error — there is no form open at that
 * point.
 */
export function VariantsTable({
  productId,
  variants,
  attributeDefinitions,
  canWrite,
}: {
  productId: string;
  variants: Variant[];
  attributeDefinitions: AttributeDefinition[];
  canWrite: boolean;
}) {
  const { t } = useTranslation();
  const { notification } = App.useApp();
  const queryClient = useQueryClient();
  const formRef = useRef<FormInstance<VariantEditFormValues>>(null);
  const [editing, setEditing] = useState<Variant | null>(null);

  function invalidate() {
    return queryClient.invalidateQueries({ queryKey: ["variants", productId] });
  }

  const patchMutation = useMutation({
    mutationFn: ({ id, body }: { id: string; body: VariantPatch }) => updateVariant(id, body),
    onSuccess: async () => {
      await invalidate();
      setEditing(null);
      notification.success({ message: t("catalog.variants.saved") });
    },
    onError: (error) => notifyApiError(notification, error, t),
  });

  const activeMutation = useMutation({
    mutationFn: ({ id, isActive }: { id: string; isActive: boolean }) =>
      updateVariant(id, { isActive }),
    onSuccess: () => invalidate(),
    onError: (error) => notifyApiError(notification, error, t),
  });

  const deleteMutation = useMutation({
    mutationFn: (id: string) => deleteVariant(id),
    onSuccess: () => invalidate(),
    onError: (error) => {
      if (error instanceof ApiError && error.code === "VALIDATION_FAILED") {
        const fields = (error.details as { fields?: Record<string, string> }).fields;
        if (fields?.variantId) {
          notification.error({ message: t("catalog.variants.deleteOnly") });
          return;
        }
      }
      notifyApiError(notification, error, t);
    },
  });

  const columns: ColumnsType<Variant> = [
    ...attributeDefinitions.map((def) => ({
      title: def.name,
      key: def.code,
      render: (_: unknown, row: Variant) => row.attributes[def.code] ?? "—",
    })),
    {
      title: t("catalog.variants.columns.sku"),
      dataIndex: "sku",
      render: (v: string | null) => v ?? "—",
    },
    {
      title: t("catalog.variants.columns.barcode"),
      dataIndex: "barcode",
      render: (v: string | null) => v ?? "—",
    },
    {
      title: t("catalog.variants.columns.priceOverride"),
      dataIndex: "priceOverride",
      render: (v: string | null) => v ?? "—",
    },
    ...(variants.some((v) => v.costOverride !== undefined)
      ? [
          {
            title: t("catalog.variants.columns.costOverride"),
            key: "costOverride",
            render: (_: unknown, row: Variant) => row.costOverride ?? "—",
          },
        ]
      : []),
    {
      title: t("catalog.variants.columns.active"),
      key: "isActive",
      render: (_: unknown, row: Variant) => (
        <Switch
          checked={row.isActive}
          disabled={!canWrite}
          loading={activeMutation.isPending && activeMutation.variables?.id === row.id}
          onChange={(checked) => activeMutation.mutate({ id: row.id, isActive: checked })}
        />
      ),
    },
    ...(canWrite
      ? [
          {
            title: "",
            key: "actions",
            render: (_: unknown, row: Variant) => (
              <>
                <Button size="small" onClick={() => setEditing(row)}>
                  {t("catalog.variants.edit")}
                </Button>{" "}
                <Popconfirm
                  title={t("catalog.variants.confirmDelete")}
                  onConfirm={() => deleteMutation.mutate(row.id)}
                >
                  <Button size="small" danger>
                    {t("catalog.variants.delete")}
                  </Button>
                </Popconfirm>
              </>
            ),
          },
        ]
      : []),
  ];

  return (
    <>
      <Table<Variant>
        rowKey="id"
        columns={columns}
        dataSource={variants}
        pagination={false}
        size="small"
      />

      <Drawer
        title={t("catalog.variants.edit")}
        open={editing != null}
        onClose={() => setEditing(null)}
        extra={
          <Button
            type="primary"
            loading={patchMutation.isPending}
            onClick={() => formRef.current?.submit()}
          >
            {t("common.save")}
          </Button>
        }
      >
        {editing && (
          <VariantEditForm
            key={editing.id}
            ref={formRef}
            variant={editing}
            attributeDefinitions={attributeDefinitions}
            onFinish={(values) => {
              const body = buildVariantPatch(
                editing,
                values,
                attributeDefinitions.map((def) => def.code),
              );
              patchMutation.mutate({ id: editing.id, body });
            }}
          />
        )}
      </Drawer>
    </>
  );
}
