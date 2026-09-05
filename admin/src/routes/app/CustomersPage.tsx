import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import {
  App,
  Button,
  Card,
  Drawer,
  Form,
  Input,
  Modal,
  Popconfirm,
  Select,
  Space,
  Table,
  Tag,
} from "antd";
import type { ColumnsType } from "antd/es/table";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import { useAuth } from "../../auth/AuthContext";
import {
  type Customer,
  type CustomerCreate,
  type CustomerPatch,
  createCustomer,
  deleteCustomer,
  fetchCustomersPage,
  updateCustomer,
} from "../../customers/api";
import { applyApiErrorToForm, notifyApiError } from "../../lib/errors";
import { useCursorList } from "../../lib/useCursorList";

/** Matches `SuppliersPage`'s search minimum (`docs/05-API.md` §
 * Conventions: free-text search is ILIKE/trigram). */
const MIN_QUERY_LENGTH = 2;
const SEARCH_DEBOUNCE_MS = 300;

interface CustomerFormValues {
  fullName: string;
  phone?: string;
  telegramUsername?: string;
  note?: string;
  tags?: string[];
}

/**
 * List, search, create, edit and soft-delete customers (`docs/04-DATA-MODEL.md`
 * § 7: cashier+ may create/read, manager+ may edit/delete — the server
 * enforces it via `customers.write`; this page only hides the edit/delete
 * controls for a role that lacks it, same as `ProductsListPage`'s
 * `catalog.write` gating). Each row links to `CustomerDetailPage` for that
 * customer's purchase history.
 */
export function CustomersPage() {
  const { t } = useTranslation();
  const { notification } = App.useApp();
  const { can } = useAuth();
  const canWrite = can("customers.write");
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  const [createForm] = Form.useForm<CustomerFormValues>();
  const [editForm] = Form.useForm<CustomerFormValues>();

  const [createOpen, setCreateOpen] = useState(false);
  const [editingCustomer, setEditingCustomer] = useState<Customer | null>(null);

  const [rawQuery, setRawQuery] = useState("");
  const [debouncedQuery, setDebouncedQuery] = useState("");

  useEffect(() => {
    const trimmed = rawQuery.trim();
    if (trimmed.length > 0 && trimmed.length < MIN_QUERY_LENGTH) {
      return;
    }
    const timer = setTimeout(() => setDebouncedQuery(trimmed), SEARCH_DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [rawQuery]);

  const filters = { q: debouncedQuery || undefined };

  const { data, fetchNextPage, hasNextPage, isFetchingNextPage, isPending } = useCursorList(
    ["customers", filters],
    (cursor) => fetchCustomersPage(filters, cursor),
  );
  const customers = data?.pages.flatMap((page) => page.items) ?? [];

  function invalidateCustomers() {
    return queryClient.invalidateQueries({ queryKey: ["customers"] });
  }

  const createMutation = useMutation({
    mutationFn: (values: CustomerFormValues) => {
      const body: CustomerCreate = {
        fullName: values.fullName,
        ...(values.phone ? { phone: values.phone } : {}),
        ...(values.telegramUsername ? { telegramUsername: values.telegramUsername } : {}),
        ...(values.note ? { note: values.note } : {}),
        ...(values.tags && values.tags.length > 0 ? { tags: values.tags } : {}),
      };
      return createCustomer(body);
    },
    onSuccess: async () => {
      await invalidateCustomers();
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
    mutationFn: ({ id, body }: { id: string; body: CustomerPatch }) => updateCustomer(id, body),
    onSuccess: async () => {
      await invalidateCustomers();
      setEditingCustomer(null);
    },
    onError: (error) => {
      if (!applyApiErrorToForm(editForm, error, t)) {
        notifyApiError(notification, error, t);
      }
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (id: string) => deleteCustomer(id),
    onSuccess: () => invalidateCustomers(),
    onError: (error) => notifyApiError(notification, error, t),
  });

  const columns: ColumnsType<Customer> = [
    { title: t("customers.columns.fullName"), dataIndex: "fullName" },
    {
      title: t("customers.columns.phone"),
      dataIndex: "phone",
      render: (value: string | null) => value ?? "—",
    },
    {
      title: t("customers.columns.telegramUsername"),
      dataIndex: "telegramUsername",
      render: (value: string | null) => value ?? "—",
    },
    {
      title: t("customers.columns.tags"),
      dataIndex: "tags",
      render: (value: string[]) =>
        value.length > 0 ? (
          <Space size={4} wrap>
            {value.map((tag) => (
              <Tag key={tag}>{tag}</Tag>
            ))}
          </Space>
        ) : (
          "—"
        ),
    },
    {
      title: "",
      key: "actions",
      render: (_, row) => (
        <Space>
          <Button
            size="small"
            onClick={() => navigate({ to: "/customers/$id", params: { id: row.id } })}
          >
            {t("customers.view")}
          </Button>
          {canWrite && (
            <Button
              size="small"
              onClick={() => {
                setEditingCustomer(row);
                editForm.setFieldsValue({
                  fullName: row.fullName,
                  phone: row.phone ?? undefined,
                  telegramUsername: row.telegramUsername ?? undefined,
                  note: row.note ?? undefined,
                  tags: row.tags,
                });
              }}
            >
              {t("customers.edit")}
            </Button>
          )}
          {canWrite && (
            <Popconfirm
              title={t("customers.confirmDelete")}
              onConfirm={() => deleteMutation.mutate(row.id)}
            >
              <Button size="small" danger>
                {t("customers.delete")}
              </Button>
            </Popconfirm>
          )}
        </Space>
      ),
    },
  ];

  return (
    <Card
      title={t("customers.title")}
      extra={
        <Button type="primary" onClick={() => setCreateOpen(true)}>
          {t("customers.add")}
        </Button>
      }
    >
      <Space style={{ marginBottom: 16 }} wrap>
        <Input.Search
          allowClear
          placeholder={t("customers.searchPlaceholder")}
          value={rawQuery}
          onChange={(event) => setRawQuery(event.target.value)}
          style={{ width: 240 }}
        />
      </Space>

      <Table<Customer>
        rowKey="id"
        columns={columns}
        dataSource={customers}
        loading={isPending}
        pagination={false}
      />
      {hasNextPage && (
        <div style={{ textAlign: "center", marginTop: 16 }}>
          <Button loading={isFetchingNextPage} onClick={() => fetchNextPage()}>
            {t("common.loadMore")}
          </Button>
        </div>
      )}

      <Modal
        title={t("customers.add")}
        open={createOpen}
        onCancel={() => {
          setCreateOpen(false);
          createForm.resetFields();
        }}
        onOk={() => createForm.submit()}
        confirmLoading={createMutation.isPending}
      >
        <Form<CustomerFormValues>
          form={createForm}
          layout="vertical"
          onFinish={(values) => createMutation.mutate(values)}
        >
          <Form.Item
            name="fullName"
            label={t("customers.fields.fullName")}
            rules={[{ required: true }]}
          >
            <Input />
          </Form.Item>
          <Form.Item name="phone" label={t("customers.fields.phone")}>
            <Input />
          </Form.Item>
          <Form.Item name="telegramUsername" label={t("customers.fields.telegramUsername")}>
            <Input />
          </Form.Item>
          <Form.Item name="note" label={t("customers.fields.note")}>
            <Input.TextArea rows={3} />
          </Form.Item>
          <Form.Item name="tags" label={t("customers.fields.tags")}>
            <Select mode="tags" open={false} placeholder={t("customers.tagsPlaceholder")} />
          </Form.Item>
        </Form>
      </Modal>

      <Drawer
        title={t("customers.edit")}
        open={editingCustomer != null}
        onClose={() => setEditingCustomer(null)}
        extra={
          <Button type="primary" loading={editMutation.isPending} onClick={() => editForm.submit()}>
            {t("common.save")}
          </Button>
        }
      >
        {editingCustomer && (
          <Form<CustomerFormValues>
            form={editForm}
            layout="vertical"
            onFinish={(values) => {
              const body: CustomerPatch = {
                fullName: values.fullName,
                // D-35: a cleared Input is `""` in AntD's form state, not
                // `undefined` — normalize it to a real `null` so the API
                // clears the field (same pattern as `SuppliersPage`).
                phone: values.phone ? values.phone : null,
                telegramUsername: values.telegramUsername ? values.telegramUsername : null,
                note: values.note ? values.note : null,
                tags: values.tags ?? [],
              };
              editMutation.mutate({ id: editingCustomer.id, body });
            }}
          >
            <Form.Item
              name="fullName"
              label={t("customers.fields.fullName")}
              rules={[{ required: true }]}
            >
              <Input />
            </Form.Item>
            <Form.Item name="phone" label={t("customers.fields.phone")}>
              <Input />
            </Form.Item>
            <Form.Item name="telegramUsername" label={t("customers.fields.telegramUsername")}>
              <Input />
            </Form.Item>
            <Form.Item name="note" label={t("customers.fields.note")}>
              <Input.TextArea rows={3} />
            </Form.Item>
            <Form.Item name="tags" label={t("customers.fields.tags")}>
              <Select mode="tags" open={false} placeholder={t("customers.tagsPlaceholder")} />
            </Form.Item>
          </Form>
        )}
      </Drawer>
    </Card>
  );
}
