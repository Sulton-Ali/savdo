import { locales } from "@savdo/i18n";
import { useMutation, useQueryClient } from "@tanstack/react-query";
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
  Switch,
  Table,
  Tag,
} from "antd";
import type { ColumnsType } from "antd/es/table";
import dayjs from "dayjs";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { applyApiErrorToForm, notifyApiError } from "../../lib/errors";
import { useCursorList } from "../../lib/useCursorList";
import {
  createStaff,
  fetchStaffPage,
  type StaffCreate,
  type StaffPatch,
  setStaffPassword,
  type User,
  updateStaff,
} from "../../staff/api";

const STAFF_ROLES = ["manager", "cashier"] as const;

interface CreateFormValues {
  username: string;
  password: string;
  fullName: string;
  phone?: string;
  role: (typeof STAFF_ROLES)[number];
  locale: (typeof locales)[number];
}

interface EditFormValues {
  fullName: string;
  phone?: string;
  role: User["role"];
  locale: (typeof locales)[number];
}

interface PasswordFormValues {
  password: string;
}

function formatLastLogin(value: string | null, t: (key: string) => string): string {
  if (!value) {
    return t("staff.never");
  }
  return dayjs(value).format("YYYY-MM-DD HH:mm");
}

export function StaffPage() {
  const { t } = useTranslation();
  const { notification } = App.useApp();
  const queryClient = useQueryClient();

  const [createForm] = Form.useForm<CreateFormValues>();
  const [editForm] = Form.useForm<EditFormValues>();
  const [passwordForm] = Form.useForm<PasswordFormValues>();

  const [createOpen, setCreateOpen] = useState(false);
  const [editingUser, setEditingUser] = useState<User | null>(null);
  const [passwordUser, setPasswordUser] = useState<User | null>(null);

  const { data, fetchNextPage, hasNextPage, isFetchingNextPage, isPending } = useCursorList(
    ["staff"],
    fetchStaffPage,
  );
  const staff = data?.pages.flatMap((page) => page.items) ?? [];

  function invalidateStaff() {
    return queryClient.invalidateQueries({ queryKey: ["staff"] });
  }

  const createMutation = useMutation({
    mutationFn: (values: CreateFormValues) => {
      const body: StaffCreate = {
        username: values.username,
        password: values.password,
        fullName: values.fullName,
        role: values.role,
        locale: values.locale,
        ...(values.phone ? { phone: values.phone } : {}),
      };
      return createStaff(body);
    },
    onSuccess: async () => {
      await invalidateStaff();
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
    mutationFn: ({ id, body }: { id: string; body: StaffPatch }) => updateStaff(id, body),
    onSuccess: async () => {
      await invalidateStaff();
      setEditingUser(null);
    },
    onError: (error) => {
      if (!applyApiErrorToForm(editForm, error, t)) {
        notifyApiError(notification, error, t);
      }
    },
  });

  const toggleActiveMutation = useMutation({
    mutationFn: ({ id, isActive }: { id: string; isActive: boolean }) =>
      updateStaff(id, { isActive }),
    onSuccess: () => invalidateStaff(),
    onError: (error) => notifyApiError(notification, error, t),
  });

  const passwordMutation = useMutation({
    mutationFn: ({ id, password }: { id: string; password: string }) =>
      setStaffPassword(id, { password }),
    onSuccess: () => {
      setPasswordUser(null);
      passwordForm.resetFields();
    },
    onError: (error) => {
      if (!applyApiErrorToForm(passwordForm, error, t)) {
        notifyApiError(notification, error, t);
      }
    },
  });

  const columns: ColumnsType<User> = [
    { title: t("staff.columns.username"), dataIndex: "username" },
    { title: t("staff.columns.fullName"), dataIndex: "fullName" },
    {
      title: t("staff.columns.phone"),
      dataIndex: "phone",
      render: (phone: string | null) => phone ?? "—",
    },
    {
      title: t("staff.columns.role"),
      dataIndex: "role",
      render: (role: User["role"]) => <Tag>{t(`roles.${role}`)}</Tag>,
    },
    {
      title: t("staff.columns.active"),
      dataIndex: "isActive",
      render: (isActive: boolean, row) => {
        const isOwnerRow = row.role === "owner";
        const toggle = (
          <Switch
            checked={isActive}
            disabled={isOwnerRow || toggleActiveMutation.isPending}
            onChange={(checked) => {
              if (checked) {
                toggleActiveMutation.mutate({ id: row.id, isActive: true });
              }
            }}
          />
        );
        if (isOwnerRow || !isActive) {
          return toggle;
        }
        return (
          <Popconfirm
            title={t("staff.confirmDeactivate", { name: row.fullName })}
            onConfirm={() => toggleActiveMutation.mutate({ id: row.id, isActive: false })}
          >
            <Switch checked={isActive} disabled={toggleActiveMutation.isPending} />
          </Popconfirm>
        );
      },
    },
    {
      title: t("staff.columns.lastLogin"),
      dataIndex: "lastLoginAt",
      render: (value: string | null) => formatLastLogin(value, t),
    },
    {
      title: "",
      key: "actions",
      render: (_, row) => (
        <Space>
          <Button
            size="small"
            onClick={() => {
              setEditingUser(row);
              editForm.setFieldsValue({
                fullName: row.fullName,
                phone: row.phone ?? undefined,
                role: row.role,
                locale: row.locale,
              });
            }}
          >
            {t("staff.edit")}
          </Button>
          <Button
            size="small"
            onClick={() => {
              setPasswordUser(row);
              passwordForm.resetFields();
            }}
          >
            {t("staff.setPassword")}
          </Button>
        </Space>
      ),
    },
  ];

  return (
    <Card
      title={t("staff.title")}
      extra={
        <Button type="primary" onClick={() => setCreateOpen(true)}>
          {t("staff.add")}
        </Button>
      }
    >
      <Table<User>
        rowKey="id"
        columns={columns}
        dataSource={staff}
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
        title={t("staff.add")}
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
          initialValues={{ role: "cashier", locale: "uz" }}
          onFinish={(values) => createMutation.mutate(values)}
        >
          <Form.Item
            name="username"
            label={t("staff.fields.username")}
            rules={[{ required: true, min: 3, max: 64 }]}
          >
            <Input autoComplete="off" />
          </Form.Item>
          <Form.Item
            name="password"
            label={t("staff.fields.password")}
            rules={[{ required: true, min: 8, max: 128 }]}
          >
            <Input.Password autoComplete="new-password" />
          </Form.Item>
          <Form.Item
            name="fullName"
            label={t("staff.fields.fullName")}
            rules={[{ required: true }]}
          >
            <Input />
          </Form.Item>
          <Form.Item name="phone" label={t("staff.fields.phone")} extra={t("staff.phoneHint")}>
            <Input />
          </Form.Item>
          <Form.Item name="role" label={t("staff.fields.role")} rules={[{ required: true }]}>
            <Select
              options={STAFF_ROLES.map((role) => ({ value: role, label: t(`roles.${role}`) }))}
            />
          </Form.Item>
          <Form.Item name="locale" label={t("staff.fields.locale")} rules={[{ required: true }]}>
            <Select
              options={locales.map((locale) => ({ value: locale, label: t(`lang.${locale}`) }))}
            />
          </Form.Item>
        </Form>
      </Modal>

      <Drawer
        title={t("staff.edit")}
        open={editingUser != null}
        onClose={() => setEditingUser(null)}
        extra={
          <Button type="primary" loading={editMutation.isPending} onClick={() => editForm.submit()}>
            {t("common.save")}
          </Button>
        }
      >
        {editingUser && (
          <Form<EditFormValues>
            form={editForm}
            layout="vertical"
            onFinish={(values) => {
              const body: StaffPatch = {
                fullName: values.fullName,
                phone: values.phone ?? null,
                locale: values.locale,
                // The owner's role is never patched — the Select above is
                // disabled for that row and the API rejects it either way.
                ...(editingUser.role === "owner"
                  ? {}
                  : { role: values.role as StaffPatch["role"] }),
              };
              editMutation.mutate({ id: editingUser.id, body });
            }}
          >
            <Form.Item
              name="fullName"
              label={t("staff.fields.fullName")}
              rules={[{ required: true }]}
            >
              <Input />
            </Form.Item>
            <Form.Item name="phone" label={t("staff.fields.phone")} extra={t("staff.phoneHint")}>
              <Input />
            </Form.Item>
            <Form.Item name="role" label={t("staff.fields.role")} rules={[{ required: true }]}>
              <Select
                disabled={editingUser.role === "owner"}
                options={
                  editingUser.role === "owner"
                    ? [{ value: "owner", label: t("roles.owner") }]
                    : STAFF_ROLES.map((role) => ({ value: role, label: t(`roles.${role}`) }))
                }
              />
            </Form.Item>
            <Form.Item name="locale" label={t("staff.fields.locale")} rules={[{ required: true }]}>
              <Select
                options={locales.map((locale) => ({ value: locale, label: t(`lang.${locale}`) }))}
              />
            </Form.Item>
          </Form>
        )}
      </Drawer>

      <Modal
        title={t("staff.setPassword")}
        open={passwordUser != null}
        onCancel={() => setPasswordUser(null)}
        onOk={() => passwordForm.submit()}
        confirmLoading={passwordMutation.isPending}
      >
        <Form<PasswordFormValues>
          form={passwordForm}
          layout="vertical"
          onFinish={(values) => {
            if (passwordUser) {
              passwordMutation.mutate({ id: passwordUser.id, password: values.password });
            }
          }}
        >
          <Form.Item
            name="password"
            label={t("staff.fields.newPassword")}
            rules={[{ required: true, min: 8, max: 128 }]}
          >
            <Input.Password autoComplete="new-password" />
          </Form.Item>
        </Form>
      </Modal>
    </Card>
  );
}
