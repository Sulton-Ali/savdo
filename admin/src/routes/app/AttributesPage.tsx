import { type Locale, locales } from "@savdo/i18n";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  App,
  Button,
  Card,
  Drawer,
  Form,
  type FormInstance,
  Input,
  InputNumber,
  Modal,
  Table,
  Tabs,
} from "antd";
import type { ColumnsType } from "antd/es/table";
import { forwardRef, useImperativeHandle, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import { useAuth } from "../../auth/AuthContext";
import {
  type AttributeDefinition,
  type AttributeDefinitionCreate,
  type AttributeDefinitionPatch,
  createAttributeDefinition,
  fetchAttributeDefinitions,
  updateAttributeDefinition,
} from "../../catalog/api";
import { applyApiErrorToForm, notifyApiError } from "../../lib/errors";
import {
  buildTranslationsForCreate,
  buildTranslationsForPatch,
  type TranslationsFormValue,
} from "../../lib/translations";

interface CreateFormValues {
  code?: string;
  sortOrder?: number;
  translations?: TranslationsFormValue;
}

interface EditFormValues {
  sortOrder?: number;
  translations?: TranslationsFormValue;
}

/** The translations tabs shared by the create modal and edit drawer — just
 * `name` per locale, no `description` (attribute labels are short, e.g.
 * "Size", "Colour"). The shop's own default locale is required, matching
 * every other translated form (categories, products). */
function TranslationTabs({ defaultLocale }: { defaultLocale: Locale }) {
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
            rules={[{ required: locale === defaultLocale }]}
          >
            <Input />
          </Form.Item>
        ),
      }))}
    />
  );
}

/**
 * The edit Drawer's form body, mounted fresh (via the parent's `key`) for
 * every attribute definition it edits, reading its starting values from
 * `Form`'s own `initialValues` rather than a `setFieldsValue` call after
 * mount — the latter marks already-registered fields "touched", making
 * `buildTranslationsForPatch` think the user edited a locale they never
 * opened (T6a review, MAJOR 1).
 */
const AttributeEditForm = forwardRef<
  FormInstance<EditFormValues>,
  {
    attribute: AttributeDefinition;
    defaultLocale: Locale;
    onFinish: (values: EditFormValues, form: FormInstance<EditFormValues>) => void;
  }
>(function AttributeEditForm({ attribute, defaultLocale, onFinish }, ref) {
  const { t } = useTranslation();
  const [form] = Form.useForm<EditFormValues>();
  useImperativeHandle(ref, () => form, [form]);

  return (
    <Form<EditFormValues>
      form={form}
      layout="vertical"
      initialValues={{ sortOrder: attribute.sortOrder, translations: attribute.translations }}
      onFinish={(values) => onFinish(values, form)}
    >
      <Form.Item label={t("catalog.attributes.fields.code")}>
        <Input value={attribute.code} disabled />
      </Form.Item>
      <Form.Item name="sortOrder" label={t("catalog.attributes.fields.sortOrder")}>
        <InputNumber style={{ width: "100%" }} />
      </Form.Item>
      <TranslationTabs defaultLocale={defaultLocale} />
    </Form>
  );
});

export function AttributesPage() {
  const { t } = useTranslation();
  const { notification } = App.useApp();
  const { me } = useAuth();
  const queryClient = useQueryClient();

  const [createForm] = Form.useForm<CreateFormValues>();
  const editFormRef = useRef<FormInstance<EditFormValues>>(null);
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
    mutationFn: (values: CreateFormValues) => {
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
      const form = editFormRef.current;
      if (!form || !applyApiErrorToForm(form, error, t)) {
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
        <Button size="small" onClick={() => setEditing(row)}>
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
        <Form<CreateFormValues>
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
          <TranslationTabs defaultLocale={me.shop.defaultLocale} />
        </Form>
      </Modal>

      <Drawer
        title={t("catalog.attributes.edit")}
        open={editing != null}
        onClose={() => setEditing(null)}
        extra={
          <Button
            type="primary"
            loading={editMutation.isPending}
            onClick={() => editFormRef.current?.submit()}
          >
            {t("common.save")}
          </Button>
        }
      >
        {editing && (
          <AttributeEditForm
            key={editing.id}
            ref={editFormRef}
            attribute={editing}
            defaultLocale={me.shop.defaultLocale}
            onFinish={(values, form) => {
              const body: AttributeDefinitionPatch = {
                ...(values.sortOrder != null ? { sortOrder: values.sortOrder } : {}),
              };
              const translations = buildTranslationsForPatch(form, values.translations);
              if (translations) {
                body.translations = translations;
              }
              editMutation.mutate({ id: editing.id, body });
            }}
          />
        )}
      </Drawer>
    </Card>
  );
}
