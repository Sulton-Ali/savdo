import { locales } from "@savdo/i18n";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  App,
  Button,
  Card,
  Drawer,
  Form,
  Input,
  InputNumber,
  Popconfirm,
  Space,
  Switch,
  Tabs,
  Tag,
  Tree,
} from "antd";
import type { DataNode } from "antd/es/tree";
import { Pencil, Plus, Trash2 } from "lucide-react";
import { type ReactNode, useCallback, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { useAuth } from "../../auth/AuthContext";
import {
  type Category,
  type CategoryCreate,
  type CategoryPatch,
  createCategory,
  deleteCategory,
  fetchCategories,
  updateCategory,
} from "../../catalog/api";
import { buildCategoryTree, categoryDepth, MAX_CATEGORY_DEPTH } from "../../catalog/tree";
import { ApiError, applyApiErrorToForm, notifyApiError } from "../../lib/errors";
import { buildTranslationsForCreate, buildTranslationsForPatch } from "../../lib/translations";

interface FormValues {
  slug?: string;
  sortOrder?: number;
  isActive?: boolean;
  translations?: Record<string, { name?: string; description?: string }>;
}

/** `{ type: "create", parentId }` opens the create Drawer for a new root
 * category (`parentId: null`) or a child of `parentId`; `{ type: "edit",
 * category }` opens it prefilled for that category. */
type DrawerState =
  | { type: "create"; parentId: string | null }
  | { type: "edit"; category: Category };

export function CategoriesPage() {
  const { t } = useTranslation();
  const { notification } = App.useApp();
  const queryClient = useQueryClient();
  const { me } = useAuth();

  const [form] = Form.useForm<FormValues>();
  const [drawer, setDrawer] = useState<DrawerState | null>(null);

  const { data, isPending } = useQuery({ queryKey: ["categories"], queryFn: fetchCategories });
  const categories = useMemo(() => data ?? [], [data]);

  function invalidate() {
    return queryClient.invalidateQueries({ queryKey: ["categories"] });
  }

  const saveMutation = useMutation({
    mutationFn: (values: FormValues) => {
      if (!drawer) {
        throw new Error("no category form open");
      }
      if (drawer.type === "create") {
        const body: CategoryCreate = {
          translations: buildTranslationsForCreate(values.translations),
          ...(drawer.parentId ? { parentId: drawer.parentId } : {}),
          ...(values.slug?.trim() ? { slug: values.slug.trim() } : {}),
          ...(values.sortOrder != null ? { sortOrder: values.sortOrder } : {}),
          ...(typeof values.isActive === "boolean" ? { isActive: values.isActive } : {}),
        };
        return createCategory(body);
      }
      const body: CategoryPatch = {
        ...(values.slug?.trim() ? { slug: values.slug.trim() } : {}),
        ...(values.sortOrder != null ? { sortOrder: values.sortOrder } : {}),
        ...(typeof values.isActive === "boolean" ? { isActive: values.isActive } : {}),
      };
      const translations = buildTranslationsForPatch(form, values.translations);
      if (translations) {
        body.translations = translations;
      }
      return updateCategory(drawer.category.id, body);
    },
    onSuccess: async () => {
      await invalidate();
      setDrawer(null);
    },
    onError: (error) => {
      if (!applyApiErrorToForm(form, error, t)) {
        notifyApiError(notification, error, t);
      }
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (id: string) => deleteCategory(id),
    onSuccess: () => invalidate(),
    onError: (error) => {
      if (
        error instanceof ApiError &&
        error.code === "CONFLICT" &&
        error.details.field === "products"
      ) {
        notification.error({ message: t("catalog.errors.categoryHasProducts") });
        return;
      }
      notifyApiError(notification, error, t);
    },
  });

  const openCreate = useCallback(
    (parentId: string | null) => {
      form.resetFields();
      form.setFieldsValue({ isActive: true });
      setDrawer({ type: "create", parentId });
    },
    [form],
  );

  const openEdit = useCallback(
    (category: Category) => {
      form.resetFields();
      form.setFieldsValue({
        slug: category.slug,
        sortOrder: category.sortOrder,
        isActive: category.isActive,
        translations: category.translations,
      });
      setDrawer({ type: "edit", category });
    },
    [form],
  );

  const renderTitle = useCallback(
    (category: Category): ReactNode => {
      const depth = categoryDepth(category.id, categories);
      return (
        <Space>
          <span>{category.name}</span>
          {!category.isActive && <Tag>{t("catalog.categories.inactive")}</Tag>}
          <Button
            type="text"
            size="small"
            icon={<Plus size={14} />}
            disabled={depth >= MAX_CATEGORY_DEPTH}
            title={t("catalog.categories.addChild")}
            onClick={(event) => {
              event.stopPropagation();
              openCreate(category.id);
            }}
          />
          <Button
            type="text"
            size="small"
            icon={<Pencil size={14} />}
            title={t("catalog.categories.edit")}
            onClick={(event) => {
              event.stopPropagation();
              openEdit(category);
            }}
          />
          <Popconfirm
            title={t("catalog.categories.confirmDelete")}
            onConfirm={() => deleteMutation.mutate(category.id)}
          >
            <Button
              type="text"
              size="small"
              danger
              icon={<Trash2 size={14} />}
              title={t("catalog.categories.delete")}
              onClick={(event) => event.stopPropagation()}
            />
          </Popconfirm>
        </Space>
      );
    },
    [categories, t, openCreate, openEdit, deleteMutation.mutate],
  );

  const treeData: DataNode[] = useMemo(() => {
    function toDataNode(node: ReturnType<typeof buildCategoryTree>[number]): DataNode {
      return {
        key: node.key,
        title: renderTitle(node.category),
        children: node.children.map(toDataNode),
      };
    }
    return buildCategoryTree(categories).map(toDataNode);
  }, [categories, renderTitle]);

  // Always show the full hierarchy — `defaultExpandAll` only computes once,
  // at mount, so it misses nodes that arrive after the initial (empty)
  // render while `GET /categories` is still in flight. A depth-3-capped
  // category tree is small enough that a management page has no real need
  // for a collapse feature anyway.
  const expandedKeys = useMemo(() => categories.map((category) => category.id), [categories]);

  return (
    <Card
      title={t("catalog.categories.title")}
      extra={
        <Button type="primary" onClick={() => openCreate(null)}>
          {t("catalog.categories.addRoot")}
        </Button>
      }
    >
      <Tree
        treeData={treeData}
        expandedKeys={expandedKeys}
        blockNode
        showLine
        selectable={false}
        // Categories are capped at depth 3 (docs/04-DATA-MODEL.md § 2) — the
        // tree is always small, so plain rendering avoids rc-tree's
        // virtualization relying on a real scroll container height (0 in
        // jsdom, which would only render the first visible node in tests).
        virtual={false}
      />
      {!isPending && categories.length === 0 && <p>{t("common.comingSoon")}</p>}

      <Drawer
        title={
          drawer?.type === "edit" ? t("catalog.categories.edit") : t("catalog.categories.addRoot")
        }
        open={drawer != null}
        onClose={() => setDrawer(null)}
        extra={
          <Button type="primary" loading={saveMutation.isPending} onClick={() => form.submit()}>
            {t("common.save")}
          </Button>
        }
      >
        {drawer && (
          <Form<FormValues>
            form={form}
            layout="vertical"
            onFinish={(values) => saveMutation.mutate(values)}
          >
            <Tabs
              items={locales.map((locale) => ({
                key: locale,
                label: t(`lang.${locale}`),
                children: (
                  <>
                    <Form.Item
                      name={["translations", locale, "name"]}
                      label={t("catalog.categories.fields.name")}
                      rules={[{ required: locale === me.shop.defaultLocale }]}
                    >
                      <Input />
                    </Form.Item>
                    <Form.Item
                      name={["translations", locale, "description"]}
                      label={t("catalog.categories.fields.description")}
                    >
                      <Input.TextArea rows={3} />
                    </Form.Item>
                  </>
                ),
              }))}
            />
            <Form.Item name="slug" label={t("catalog.categories.fields.slug")}>
              <Input />
            </Form.Item>
            <Form.Item name="sortOrder" label={t("catalog.categories.fields.sortOrder")}>
              <InputNumber style={{ width: "100%" }} />
            </Form.Item>
            <Form.Item
              name="isActive"
              label={t("catalog.categories.fields.isActive")}
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
