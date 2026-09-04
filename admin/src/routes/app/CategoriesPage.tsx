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
  Popconfirm,
  Space,
  Switch,
  Tag,
  Tooltip,
  Tree,
} from "antd";
import type { DataNode } from "antd/es/tree";
import { Pencil, Plus, Trash2 } from "lucide-react";
import {
  forwardRef,
  type ReactNode,
  useCallback,
  useImperativeHandle,
  useMemo,
  useRef,
  useState,
} from "react";
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
import {
  buildTranslationsForCreate,
  buildTranslationsForPatch,
  type TranslationsFormValue,
} from "../../lib/translations";

interface CategoryFormValues {
  slug?: string;
  sortOrder?: number;
  isActive?: boolean;
  translations?: TranslationsFormValue;
}

/** `{ type: "create", parentId }` opens the create Drawer for a new root
 * category (`parentId: null`) or a child of `parentId`; `{ type: "edit",
 * category }` opens it prefilled for that category. */
type DrawerState =
  | { type: "create"; parentId: string | null }
  | { type: "edit"; category: Category };

/**
 * The Drawer's form body, mounted fresh (via the parent's `key`) for every
 * entity it edits — never reused across two different categories. It reads
 * its starting values from `Form`'s own `initialValues`, never from
 * `form.setFieldsValue` after mount: calling `setFieldsValue` on a form
 * whose fields are already registered marks them "touched", which would
 * make `buildTranslationsForPatch` think the user edited a locale they
 * never opened (T6a review, MAJOR 1).
 */
const CategoryDrawerForm = forwardRef<
  FormInstance<CategoryFormValues>,
  {
    drawer: DrawerState;
    defaultLocale: Locale;
    onFinish: (values: CategoryFormValues, form: FormInstance<CategoryFormValues>) => void;
  }
>(function CategoryDrawerForm({ drawer, defaultLocale, onFinish }, ref) {
  const { t } = useTranslation();
  const [form] = Form.useForm<CategoryFormValues>();
  useImperativeHandle(ref, () => form, [form]);

  const initialValues: CategoryFormValues =
    drawer.type === "edit"
      ? {
          slug: drawer.category.slug,
          sortOrder: drawer.category.sortOrder,
          isActive: drawer.category.isActive,
          translations: drawer.category.translations,
        }
      : { isActive: true };

  return (
    <Form<CategoryFormValues>
      form={form}
      layout="vertical"
      initialValues={initialValues}
      onFinish={(values) => onFinish(values, form)}
    >
      {locales.map((locale) => (
        <Form.Item
          key={`name-${locale}`}
          name={["translations", locale, "name"]}
          label={t("common.fieldWithLang", {
            field: t("catalog.categories.fields.name"),
            lang: t(`lang.${locale}`),
          })}
          rules={[{ required: locale === defaultLocale }]}
        >
          <Input />
        </Form.Item>
      ))}
      {locales.map((locale) => (
        <Form.Item
          key={`description-${locale}`}
          name={["translations", locale, "description"]}
          label={t("common.fieldWithLang", {
            field: t("catalog.categories.fields.description"),
            lang: t(`lang.${locale}`),
          })}
        >
          <Input.TextArea rows={3} />
        </Form.Item>
      ))}
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
  );
});

export function CategoriesPage() {
  const { t } = useTranslation();
  const { notification } = App.useApp();
  const queryClient = useQueryClient();
  const { me } = useAuth();

  const formRef = useRef<FormInstance<CategoryFormValues>>(null);
  const [drawer, setDrawer] = useState<DrawerState | null>(null);

  // Always includes inactive categories — this page is only reachable with
  // `catalog.write`, which must see and manage inactive categories too
  // (unlike the product list's cashier-hidden toggle).
  const { data, isPending } = useQuery({
    queryKey: ["categories", true],
    queryFn: () => fetchCategories(true),
  });
  const categories = useMemo(() => data ?? [], [data]);

  function invalidate() {
    return queryClient.invalidateQueries({ queryKey: ["categories"] });
  }

  const saveMutation = useMutation({
    mutationFn: ({
      values,
      form,
    }: {
      values: CategoryFormValues;
      form: FormInstance<CategoryFormValues>;
    }) => {
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
      const form = formRef.current;
      if (!form || !applyApiErrorToForm(form, error, t)) {
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

  const openCreate = useCallback((parentId: string | null) => {
    setDrawer({ type: "create", parentId });
  }, []);

  const openEdit = useCallback((category: Category) => {
    setDrawer({ type: "edit", category });
  }, []);

  const renderTitle = useCallback(
    (category: Category): ReactNode => {
      const depth = categoryDepth(category.id, categories);
      return (
        <Space>
          <span>{category.name}</span>
          {!category.isActive && <Tag>{t("catalog.categories.inactive")}</Tag>}
          <Tooltip
            title={
              depth >= MAX_CATEGORY_DEPTH ? t("catalog.categories.maxDepthReached") : undefined
            }
          >
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
          </Tooltip>
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
          <Button
            type="primary"
            loading={saveMutation.isPending}
            onClick={() => formRef.current?.submit()}
          >
            {t("common.save")}
          </Button>
        }
      >
        {drawer && (
          <CategoryDrawerForm
            key={
              drawer.type === "edit"
                ? `edit-${drawer.category.id}`
                : `create-${drawer.parentId ?? "root"}`
            }
            ref={formRef}
            drawer={drawer}
            defaultLocale={me.shop.defaultLocale}
            onFinish={(values, form) => saveMutation.mutate({ values, form })}
          />
        )}
      </Drawer>
    </Card>
  );
}
