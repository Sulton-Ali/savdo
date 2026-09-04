import { locales } from "@savdo/i18n";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { App, Button, Card, Drawer, Form, Input, InputNumber, Modal, Table, Tabs } from "antd";
import type { ColumnsType } from "antd/es/table";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import {
  type AttributeDefinition,
  type AttributeDefinitionCreate,
  type AttributeDefinitionPatch,
  createAttributeDefinition,
  fetchAttributeDefinitions,
  updateAttributeDefinition,
} from "../../catalog/api";
import { applyApiErrorToForm, notifyApiError } from "../../lib/errors";
import { buildTranslationsForCreate, buildTranslationsForPatch } from "../../lib/translations";

interface FormValues {
  code?: string;
  sortOrder?: number;
  translations?: Record<string, { name?: string }>;
}

/** The translations tabs shared by the create modal and edit drawer — just
 * `name` per locale, no `description` (attribute labels are short, e.g.
 * "Size", "Colour"). */
function TranslationTabs() {
  const { t } = useTranslation();
  return (
    <Tabs
      items={locales.map((locale) => ({
        key: locale,
        label: t(`lang.${locale}`),
        children: (
          <Form.Item
            name={["translations", locale, "name"]}
            label={t("catalog.attributes.fields.name")}
          >
            <Input />
          </Form.Item>
        ),
      }))}
    />
  );
}

export function AttributesPage() {
  const { t } = useTranslation();
  const { notification } = App.useApp();
  const queryClient = useQueryClient();

  const [createForm] = Form.useForm<FormValues>();
  const [editForm] = Form.useForm<FormValues>();
  const [createOpen, setCreateOpen] = useState(false);
  const [editing, setEditing] = useState<AttributeDefinition | null>(null);

  const { data, isPending } = useQuery({
    queryKey: ["attribute-definitions"],
    queryFn: fetchAttributeDefinitions,
  });
  const attributes = data ?? [];

  function invalidate() {
    return queryClient.invalidateQueries({ queryKey: ["attribute-definitions"] });
  }

  const createMutation = useMutation({
    mutationFn: (values: FormValues) => {
      const body: AttributeDefinitionCreate = {
        code: (values.code ?? "").trim(),
        translations: buildTranslationsForCreate(values.translations),
        ...(values.sortOrder != null ? { sortOrder: values.sortOrder } : {}),
      };
      return createAttributeDefinition(body);
    },
    onSuccess: async () => {
      await invalidate();
      setCreateOpen(false);
      createForm.resetFields();
    },
    onError: (error) => {
      if (!applyApiErrorToForm(createForm, error, t)) {
        notifyApiError(notification, error, t);
      }
    },
  });

  const editMutation = useMutation({
    mutationFn: ({ id, body }: { id: string; body: AttributeDefinitionPatch }) =>
      updateAttributeDefinition(id, body),
    onSuccess: async () => {
      await invalidate();
      setEditing(null);
    },
    onError: (error) => {
      if (!applyApiErrorToForm(editForm, error, t)) {
        notifyApiError(notification, error, t);
      }
    },
  });

  const columns: ColumnsType<AttributeDefinition> = [
    { title: t("catalog.attributes.columns.code"), dataIndex: "code" },
    { title: t("catalog.attributes.columns.name"), dataIndex: "name" },
    { title: t("catalog.attributes.columns.sortOrder"), dataIndex: "sortOrder" },
    {
      title: "",
      key: "actions",
      render: (_, row) => (
        <Button
          size="small"
          onClick={() => {
            setEditing(row);
            editForm.resetFields();
            editForm.setFieldsValue({
              code: row.code,
              sortOrder: row.sortOrder,
              translations: row.translations,
            });
          }}
        >
          {t("catalog.attributes.edit")}
        </Button>
      ),
    },
  ];

  return (
    <Card
      title={t("catalog.attributes.title")}
      extra={
        <Button type="primary" onClick={() => setCreateOpen(true)}>
          {t("catalog.attributes.add")}
        </Button>
      }
    >
      <Table<AttributeDefinition>
        rowKey="id"
        columns={columns}
        dataSource={attributes}
        loading={isPending}
        pagination={false}
      />

      <Modal
        title={t("catalog.attributes.add")}
        open={createOpen}
        onCancel={() => {
          setCreateOpen(false);
          createForm.resetFields();
        }}
        onOk={() => createForm.submit()}
        confirmLoading={createMutation.isPending}
      >
        <Form<FormValues>
          form={createForm}
          layout="vertical"
          onFinish={(values) => createMutation.mutate(values)}
        >
          <Form.Item
            name="code"
            label={t("catalog.attributes.fields.code")}
            rules={[{ required: true }]}
          >
            <Input autoComplete="off" />
          </Form.Item>
          <Form.Item name="sortOrder" label={t("catalog.attributes.fields.sortOrder")}>
            <InputNumber style={{ width: "100%" }} />
          </Form.Item>
          <TranslationTabs />
        </Form>
      </Modal>

      <Drawer
        title={t("catalog.attributes.edit")}
        open={editing != null}
        onClose={() => setEditing(null)}
        extra={
          <Button type="primary" loading={editMutation.isPending} onClick={() => editForm.submit()}>
            {t("common.save")}
          </Button>
        }
      >
        {editing && (
          <Form<FormValues>
            form={editForm}
            layout="vertical"
            onFinish={(values) => {
              const body: AttributeDefinitionPatch = {
                ...(values.sortOrder != null ? { sortOrder: values.sortOrder } : {}),
              };
              const translations = buildTranslationsForPatch(editForm, values.translations);
              if (translations) {
                body.translations = translations;
              }
              editMutation.mutate({ id: editing.id, body });
            }}
          >
            <Form.Item name="code" label={t("catalog.attributes.fields.code")}>
              <Input disabled />
            </Form.Item>
            <Form.Item name="sortOrder" label={t("catalog.attributes.fields.sortOrder")}>
              <InputNumber style={{ width: "100%" }} />
            </Form.Item>
            <TranslationTabs />
          </Form>
        )}
      </Drawer>
    </Card>
  );
}
