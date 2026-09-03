import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  App,
  Badge,
  Button,
  Card,
  Drawer,
  Form,
  Input,
  Modal,
  Select,
  Switch,
  Table,
  Tag,
} from "antd";
import type { ColumnsType } from "antd/es/table";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { applyApiErrorToForm, notifyApiError } from "../../lib/errors";
import { useCursorList } from "../../lib/useCursorList";
import {
  createLocation,
  fetchLocationsPage,
  type Location,
  type LocationCreate,
  type LocationPatch,
  updateLocation,
} from "../../locations/api";

const LOCATION_KINDS = ["store", "warehouse"] as const;

interface CreateFormValues {
  name: string;
  kind: (typeof LOCATION_KINDS)[number];
  isDefault?: boolean;
}

interface EditFormValues {
  name: string;
  kind: (typeof LOCATION_KINDS)[number];
  isDefault?: boolean;
  isActive?: boolean;
}

export function LocationsPage() {
  const { t } = useTranslation();
  const { notification } = App.useApp();
  const queryClient = useQueryClient();

  const [createForm] = Form.useForm<CreateFormValues>();
  const [editForm] = Form.useForm<EditFormValues>();

  const [createOpen, setCreateOpen] = useState(false);
  const [editingLocation, setEditingLocation] = useState<Location | null>(null);

  const { data, fetchNextPage, hasNextPage, isFetchingNextPage, isPending } = useCursorList(
    ["locations"],
    fetchLocationsPage,
  );
  const locationList = data?.pages.flatMap((page) => page.items) ?? [];

  function invalidateLocations() {
    return queryClient.invalidateQueries({ queryKey: ["locations"] });
  }

  const createMutation = useMutation({
    mutationFn: (values: CreateFormValues) => {
      const body: LocationCreate = {
        name: values.name,
        kind: values.kind,
        ...(values.isDefault ? { isDefault: true } : {}),
      };
      return createLocation(body);
    },
    onSuccess: async () => {
      await invalidateLocations();
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
    mutationFn: ({ id, body }: { id: string; body: LocationPatch }) => updateLocation(id, body),
    onSuccess: async () => {
      await invalidateLocations();
      setEditingLocation(null);
    },
    onError: (error) => {
      if (!applyApiErrorToForm(editForm, error, t)) {
        notifyApiError(notification, error, t);
      }
    },
  });

  const columns: ColumnsType<Location> = [
    { title: t("locations.columns.name"), dataIndex: "name" },
    {
      title: t("locations.columns.kind"),
      dataIndex: "kind",
      render: (kind: Location["kind"]) => <Tag>{t(`locations.kinds.${kind}`)}</Tag>,
    },
    {
      title: t("locations.columns.default"),
      dataIndex: "isDefault",
      render: (isDefault: boolean) =>
        isDefault ? <Badge status="success" text={t("locations.columns.default")} /> : null,
    },
    {
      title: t("locations.columns.active"),
      dataIndex: "isActive",
      render: (isActive: boolean) => <Switch checked={isActive} disabled />,
    },
    {
      title: "",
      key: "actions",
      render: (_, row) => (
        <Button
          size="small"
          onClick={() => {
            setEditingLocation(row);
            editForm.setFieldsValue({
              name: row.name,
              kind: row.kind,
              isDefault: row.isDefault,
              isActive: row.isActive,
            });
          }}
        >
          {t("locations.edit")}
        </Button>
      ),
    },
  ];

  return (
    <Card
      title={t("locations.title")}
      extra={
        <Button type="primary" onClick={() => setCreateOpen(true)}>
          {t("locations.add")}
        </Button>
      }
    >
      <Table<Location>
        rowKey="id"
        columns={columns}
        dataSource={locationList}
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
        title={t("locations.add")}
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
          initialValues={{ kind: "store" }}
          onFinish={(values) => createMutation.mutate(values)}
        >
          <Form.Item name="name" label={t("locations.fields.name")} rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="kind" label={t("locations.fields.kind")} rules={[{ required: true }]}>
            <Select
              options={LOCATION_KINDS.map((kind) => ({
                value: kind,
                label: t(`locations.kinds.${kind}`),
              }))}
            />
          </Form.Item>
          <Form.Item
            name="isDefault"
            label={t("locations.fields.isDefault")}
            valuePropName="checked"
          >
            <Switch />
          </Form.Item>
        </Form>
      </Modal>

      <Drawer
        title={t("locations.edit")}
        open={editingLocation != null}
        onClose={() => setEditingLocation(null)}
        extra={
          <Button type="primary" loading={editMutation.isPending} onClick={() => editForm.submit()}>
            {t("common.save")}
          </Button>
        }
      >
        {editingLocation && (
          <Form<EditFormValues>
            form={editForm}
            layout="vertical"
            onFinish={(values) => editMutation.mutate({ id: editingLocation.id, body: values })}
          >
            <Form.Item name="name" label={t("locations.fields.name")} rules={[{ required: true }]}>
              <Input />
            </Form.Item>
            <Form.Item name="kind" label={t("locations.fields.kind")} rules={[{ required: true }]}>
              <Select
                options={LOCATION_KINDS.map((kind) => ({
                  value: kind,
                  label: t(`locations.kinds.${kind}`),
                }))}
              />
            </Form.Item>
            <Form.Item
              name="isDefault"
              label={t("locations.fields.isDefault")}
              valuePropName="checked"
            >
              <Switch />
            </Form.Item>
            <Form.Item
              name="isActive"
              label={t("locations.fields.isActive")}
              valuePropName="checked"
            >
              <Switch />
            </Form.Item>
          </Form>
        )}
      </Drawer>
    </Card>
  );
}
