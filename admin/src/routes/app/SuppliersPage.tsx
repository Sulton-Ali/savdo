import { useMutation, useQueryClient } from "@tanstack/react-query";
import { App, Button, Card, Drawer, Form, Input, Modal, Popconfirm, Space, Table } from "antd";
import type { ColumnsType } from "antd/es/table";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import { FilterBar } from "../../components/FilterBar";
import { applyApiErrorToForm, notifyApiError } from "../../lib/errors";
import { useCursorList } from "../../lib/useCursorList";
import { useDebouncedValue } from "../../lib/useDebouncedValue";
import {
  createSupplier,
  deleteSupplier,
  fetchSuppliersPage,
  type Supplier,
  type SupplierCreate,
  type SupplierPatch,
  updateSupplier,
} from "../../suppliers/api";
import type { SuppliersSearch } from "./suppliersRoute";

/** Matches `ProductsListPage`'s search minimum (`docs/05-API.md` §
 * Conventions: free-text search is ILIKE/trigram). */
const MIN_QUERY_LENGTH = 2;
const SEARCH_DEBOUNCE_MS = 300;

interface SupplierFormValues {
  name: string;
  contactName?: string;
  phone?: string;
  telegramUsername?: string;
  note?: string;
}

export interface SuppliersPageProps {
  /** Validated filter state from the route's search params
   * (`suppliersRoute`'s `validateSearch`). */
  search: SuppliersSearch;
  /** Replaces the filter state — the caller (`suppliersRoute`) turns this
   * into a `navigate({ search, replace: true })` call so reload and share
   * restore it, while Back leaves the page instead of undoing the search
   * one keystroke at a time (D-124). */
  onSearchChange: (next: SuppliersSearch) => void;
}

export function SuppliersPage({ search, onSearchChange }: SuppliersPageProps) {
  const { t } = useTranslation();
  const { notification } = App.useApp();
  const queryClient = useQueryClient();

  const [createForm] = Form.useForm<SupplierFormValues>();
  const [editForm] = Form.useForm<SupplierFormValues>();

  const [createOpen, setCreateOpen] = useState(false);
  const [editingSupplier, setEditingSupplier] = useState<Supplier | null>(null);

  // The input stays local state for typing responsiveness; only the
  // debounced value is pushed into the route's search params. `lastPushedQ`
  // tells the sync-from-url effect below apart an external change (Reset,
  // browser Back, reload) from the round-trip of our own push, so it does
  // not clobber what the user is still typing.
  const [rawQuery, setRawQuery] = useState(search.q ?? "");
  const debouncedQuery = useDebouncedValue(rawQuery, SEARCH_DEBOUNCE_MS);
  const lastPushedQ = useRef(search.q);

  useEffect(() => {
    if (search.q !== lastPushedQ.current) {
      lastPushedQ.current = search.q;
      setRawQuery(search.q ?? "");
    }
  }, [search.q]);

  useEffect(() => {
    const trimmed = debouncedQuery.trim();
    if (trimmed.length > 0 && trimmed.length < MIN_QUERY_LENGTH) {
      return;
    }
    const next = trimmed.length > 0 ? trimmed : undefined;
    if (next === search.q) {
      return;
    }
    lastPushedQ.current = next;
    onSearchChange({ ...search, q: next });
  }, [debouncedQuery, search, onSearchChange]);

  const filters = { q: search.q };

  const { data, fetchNextPage, hasNextPage, isFetchingNextPage, isPending } = useCursorList(
    ["suppliers", filters],
    (cursor) => fetchSuppliersPage(filters, cursor),
  );
  const suppliers = data?.pages.flatMap((page) => page.items) ?? [];

  function invalidateSuppliers() {
    return queryClient.invalidateQueries({ queryKey: ["suppliers"] });
  }

  const createMutation = useMutation({
    mutationFn: (values: SupplierFormValues) => {
      const body: SupplierCreate = {
        name: values.name,
        ...(values.contactName ? { contactName: values.contactName } : {}),
        ...(values.phone ? { phone: values.phone } : {}),
        ...(values.telegramUsername ? { telegramUsername: values.telegramUsername } : {}),
        ...(values.note ? { note: values.note } : {}),
      };
      return createSupplier(body);
    },
    onSuccess: async () => {
      await invalidateSuppliers();
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
    mutationFn: ({ id, body }: { id: string; body: SupplierPatch }) => updateSupplier(id, body),
    onSuccess: async () => {
      await invalidateSuppliers();
      setEditingSupplier(null);
    },
    onError: (error) => {
      if (!applyApiErrorToForm(editForm, error, t)) {
        notifyApiError(notification, error, t);
      }
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (id: string) => deleteSupplier(id),
    onSuccess: () => invalidateSuppliers(),
    onError: (error) => notifyApiError(notification, error, t),
  });

  const columns: ColumnsType<Supplier> = [
    { title: t("suppliers.columns.name"), dataIndex: "name" },
    {
      title: t("suppliers.columns.contactName"),
      dataIndex: "contactName",
      render: (value: string | null) => value ?? "—",
    },
    {
      title: t("suppliers.columns.phone"),
      dataIndex: "phone",
      render: (value: string | null) => value ?? "—",
    },
    {
      title: t("suppliers.columns.telegramUsername"),
      dataIndex: "telegramUsername",
      render: (value: string | null) => value ?? "—",
    },
    {
      title: "",
      key: "actions",
      render: (_, row) => (
        <Space>
          <Button
            size="small"
            onClick={() => {
              setEditingSupplier(row);
              editForm.setFieldsValue({
                name: row.name,
                contactName: row.contactName ?? undefined,
                phone: row.phone ?? undefined,
                telegramUsername: row.telegramUsername ?? undefined,
                note: row.note ?? undefined,
              });
            }}
          >
            {t("suppliers.edit")}
          </Button>
          <Popconfirm
            title={t("suppliers.confirmDelete")}
            onConfirm={() => deleteMutation.mutate(row.id)}
          >
            <Button size="small" danger>
              {t("suppliers.delete")}
            </Button>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  return (
    <Card
      title={t("suppliers.title")}
      extra={
        <Button type="primary" onClick={() => setCreateOpen(true)}>
          {t("suppliers.add")}
        </Button>
      }
    >
      <FilterBar
        onReset={() => {
          setRawQuery("");
          lastPushedQ.current = undefined;
          onSearchChange({ ...search, q: undefined });
        }}
        resultCount={suppliers.length}
        hasMore={hasNextPage}
      >
        <FilterBar.Field label={t("common.search")}>
          {(labelId) => (
            <Input
              allowClear
              aria-labelledby={labelId}
              placeholder={t("suppliers.searchPlaceholder")}
              value={rawQuery}
              onChange={(event) => setRawQuery(event.target.value)}
            />
          )}
        </FilterBar.Field>
      </FilterBar>

      <Table<Supplier>
        rowKey="id"
        columns={columns}
        dataSource={suppliers}
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
        title={t("suppliers.add")}
        open={createOpen}
        onCancel={() => {
          setCreateOpen(false);
          createForm.resetFields();
        }}
        onOk={() => createForm.submit()}
        confirmLoading={createMutation.isPending}
      >
        <Form<SupplierFormValues>
          form={createForm}
          layout="vertical"
          onFinish={(values) => createMutation.mutate(values)}
        >
          <Form.Item name="name" label={t("suppliers.fields.name")} rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="contactName" label={t("suppliers.fields.contactName")}>
            <Input />
          </Form.Item>
          <Form.Item name="phone" label={t("suppliers.fields.phone")}>
            <Input />
          </Form.Item>
          <Form.Item name="telegramUsername" label={t("suppliers.fields.telegramUsername")}>
            <Input />
          </Form.Item>
          <Form.Item name="note" label={t("suppliers.fields.note")}>
            <Input.TextArea rows={3} />
          </Form.Item>
        </Form>
      </Modal>

      <Drawer
        title={t("suppliers.edit")}
        open={editingSupplier != null}
        onClose={() => setEditingSupplier(null)}
        extra={
          <Button type="primary" loading={editMutation.isPending} onClick={() => editForm.submit()}>
            {t("common.save")}
          </Button>
        }
      >
        {editingSupplier && (
          <Form<SupplierFormValues>
            form={editForm}
            layout="vertical"
            onFinish={(values) => {
              const body: SupplierPatch = {
                name: values.name,
                // D-35: a cleared Input is `""` in AntD's form state, not
                // `undefined` — normalize it to a real `null` so the API
                // clears the field (staff/phone follows the same pattern).
                contactName: values.contactName ? values.contactName : null,
                phone: values.phone ? values.phone : null,
                telegramUsername: values.telegramUsername ? values.telegramUsername : null,
                note: values.note ? values.note : null,
              };
              editMutation.mutate({ id: editingSupplier.id, body });
            }}
          >
            <Form.Item name="name" label={t("suppliers.fields.name")} rules={[{ required: true }]}>
              <Input />
            </Form.Item>
            <Form.Item name="contactName" label={t("suppliers.fields.contactName")}>
              <Input />
            </Form.Item>
            <Form.Item name="phone" label={t("suppliers.fields.phone")}>
              <Input />
            </Form.Item>
            <Form.Item name="telegramUsername" label={t("suppliers.fields.telegramUsername")}>
              <Input />
            </Form.Item>
            <Form.Item name="note" label={t("suppliers.fields.note")}>
              <Input.TextArea rows={3} />
            </Form.Item>
          </Form>
        )}
      </Drawer>
    </Card>
  );
}
