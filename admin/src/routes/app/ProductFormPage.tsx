import { locales } from "@savdo/i18n";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import {
  App,
  Breadcrumb,
  Button,
  Card,
  DatePicker,
  Form,
  Input,
  InputNumber,
  Select,
  Skeleton,
  Space,
  Switch,
  Tabs,
  TreeSelect,
} from "antd";
import type { Dayjs } from "dayjs";
import dayjs from "dayjs";
import { ArrowLeft } from "lucide-react";
import { useTranslation } from "react-i18next";

import { useAuth } from "../../auth/AuthContext";
import {
  createProduct,
  fetchCategories,
  fetchProduct,
  fetchUnits,
  type ProductCreate,
  type ProductPatch,
  updateProduct,
} from "../../catalog/api";
import { buildCategoryTreeSelectData } from "../../catalog/tree";
import { applyApiErrorToForm, notifyApiError } from "../../lib/errors";
import { formatMoney, parseMoney } from "../../lib/money";
import {
  buildTranslationsForCreate,
  buildTranslationsForPatch,
  type TranslationsFormValue,
} from "../../lib/translations";
import { VariantsImagesTab } from "./VariantsImagesTab";

interface ProductFormValues {
  translations?: TranslationsFormValue;
  categoryId?: string;
  unitId?: string;
  slug?: string;
  sku?: string;
  isActive: boolean;
  isFeatured: boolean;
  basePrice?: number;
  costPrice?: number;
  promoPrice?: number;
  promoRange?: [Dayjs, Dayjs] | null;
}

/** Shared by `productNewRoute` (`productId` undefined) and `productEditRoute`
 * (`productId` set). General/Prices/Variants tabs; the General tab shows all
 * three locales' name and description fields together, each labelled with
 * its language, rather than behind nested per-locale tabs (D-38). The
 * Variants tab is disabled in create mode — variants and images both hang
 * off a product id, so it only renders `VariantsImagesTab` once the product
 * exists (T6b spec). */
export function ProductFormPage({ productId }: { productId?: string }) {
  const { t } = useTranslation();
  const { notification } = App.useApp();
  const { me, can } = useAuth();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [form] = Form.useForm<ProductFormValues>();

  const isEdit = productId != null;

  const { data: product, isPending: productPending } = useQuery({
    queryKey: ["product", productId],
    queryFn: () => fetchProduct(productId as string),
    enabled: isEdit,
  });
  // catalog.write is required to reach this page at all, so the category
  // TreeSelect always includes inactive categories (a product may already
  // reference one, and only catalog.write should see them, T6a review MAJOR 2).
  const { data: categories } = useQuery({
    queryKey: ["categories", true],
    queryFn: () => fetchCategories(true),
  });
  const { data: units } = useQuery({ queryKey: ["units"], queryFn: fetchUnits });
  const categoryOptions = buildCategoryTreeSelectData(categories ?? []);

  // `costPrice` is present only for owner/manager (ADR-010); when editing,
  // follow the loaded product's actual response shape rather than a role
  // assumption. When creating there is no response yet to check, so fall
  // back to the `cost.read` capability.
  const showCostField = isEdit ? product?.costPrice !== undefined : can("cost.read");

  // The Form below only ever mounts once `product` is loaded (the Skeleton
  // guard returns before it otherwise) — so these become the Form's actual
  // initial values, not a later `setFieldsValue` populate. That distinction
  // matters: `setFieldsValue` on an already-mounted field marks it
  // "touched", which would make `buildTranslationsForPatch` think the user
  // edited a locale they never opened.
  const initialValues: Partial<ProductFormValues> =
    isEdit && product
      ? {
          translations: product.translations,
          categoryId: product.categoryId ?? undefined,
          unitId: product.unitId,
          slug: product.slug,
          sku: product.sku ?? undefined,
          isActive: product.isActive,
          isFeatured: product.isFeatured,
          basePrice: parseMoney(product.basePrice),
          costPrice: parseMoney(product.costPrice),
          promoPrice: parseMoney(product.promoPrice),
          promoRange:
            product.promoFrom && product.promoTo
              ? [dayjs(product.promoFrom), dayjs(product.promoTo)]
              : null,
        }
      : { isActive: true, isFeatured: false };

  /** Compares two nullable ISO timestamps by the instant they represent, not
   * by string equality — `Dayjs#toISOString()` always includes milliseconds
   * (`.000Z`), which the API's stored value never has, so a raw string
   * compare would treat an untouched promo range as "changed". */
  function sameInstant(a: string | null, b: string | null): boolean {
    if (a === b) {
      return true;
    }
    if (a == null || b == null) {
      return false;
    }
    return dayjs(a).isSame(dayjs(b));
  }

  const createMutation = useMutation({
    mutationFn: (body: ProductCreate) => createProduct(body),
    onSuccess: async (created) => {
      await queryClient.invalidateQueries({ queryKey: ["products"] });
      notification.success({ message: t("catalog.products.saved") });
      await navigate({ to: "/products/$id", params: { id: created.id } });
    },
    onError: (error) => {
      if (!applyApiErrorToForm(form, error, t)) {
        notifyApiError(notification, error, t);
      }
    },
  });

  const editMutation = useMutation({
    mutationFn: (body: ProductPatch) => updateProduct(productId as string, body),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["products"] });
      await queryClient.invalidateQueries({ queryKey: ["product", productId] });
      notification.success({ message: t("catalog.products.saved") });
    },
    onError: (error) => {
      if (!applyApiErrorToForm(form, error, t)) {
        notifyApiError(notification, error, t);
      }
    },
  });

  function handleFinish(values: ProductFormValues) {
    if (!isEdit) {
      const body: ProductCreate = {
        unitId: values.unitId as string,
        basePrice: formatMoney(values.basePrice) as string,
        translations: buildTranslationsForCreate(values.translations),
        isActive: values.isActive,
        isFeatured: values.isFeatured,
        ...(values.categoryId ? { categoryId: values.categoryId } : {}),
        ...(values.slug?.trim() ? { slug: values.slug.trim() } : {}),
        ...(values.sku?.trim() ? { sku: values.sku.trim() } : {}),
        ...(showCostField && values.costPrice != null
          ? { costPrice: formatMoney(values.costPrice) }
          : {}),
        ...(values.promoPrice != null ? { promoPrice: formatMoney(values.promoPrice) } : {}),
        ...(values.promoRange?.[0] ? { promoFrom: values.promoRange[0].toISOString() } : {}),
        ...(values.promoRange?.[1] ? { promoTo: values.promoRange[1].toISOString() } : {}),
      };
      createMutation.mutate(body);
      return;
    }

    if (!product) {
      return;
    }

    const patch: ProductPatch = {};

    const normCategoryId = values.categoryId ?? null;
    if (normCategoryId !== (product.categoryId ?? null)) {
      patch.categoryId = normCategoryId;
    }

    const normSlug = values.slug?.trim();
    if (normSlug && normSlug !== product.slug) {
      patch.slug = normSlug;
    }

    const normSku = values.sku?.trim() || null;
    if (normSku !== (product.sku ?? null)) {
      patch.sku = normSku;
    }

    if (values.unitId && values.unitId !== product.unitId) {
      patch.unitId = values.unitId;
    }

    const normBasePrice = formatMoney(values.basePrice);
    if (normBasePrice && normBasePrice !== product.basePrice) {
      patch.basePrice = normBasePrice;
    }

    if (showCostField) {
      const normCost = values.costPrice != null ? (formatMoney(values.costPrice) ?? null) : null;
      if (normCost !== (product.costPrice ?? null)) {
        patch.costPrice = normCost;
      }
    }

    const normPromoPrice =
      values.promoPrice != null ? (formatMoney(values.promoPrice) ?? null) : null;
    if (normPromoPrice !== (product.promoPrice ?? null)) {
      patch.promoPrice = normPromoPrice;
    }

    const normPromoFrom = values.promoRange?.[0] ? values.promoRange[0].toISOString() : null;
    if (!sameInstant(normPromoFrom, product.promoFrom ?? null)) {
      patch.promoFrom = normPromoFrom;
    }

    const normPromoTo = values.promoRange?.[1] ? values.promoRange[1].toISOString() : null;
    if (!sameInstant(normPromoTo, product.promoTo ?? null)) {
      patch.promoTo = normPromoTo;
    }

    if (values.isActive !== product.isActive) {
      patch.isActive = values.isActive;
    }
    if (values.isFeatured !== product.isFeatured) {
      patch.isFeatured = values.isFeatured;
    }

    const translations = buildTranslationsForPatch(form, values.translations);
    if (translations) {
      patch.translations = translations;
    }

    editMutation.mutate(patch);
  }

  // "Products › <product name>" (create: "Products › New product"), plus a
  // back arrow to the list — both navigate to `productsRoute` (D-39). While
  // an existing product is still loading, the crumb falls back to the
  // generic edit title rather than waiting on `product.name`.
  const breadcrumbLabel = isEdit
    ? (product?.name ?? t("catalog.products.editTitle"))
    : t("catalog.products.createTitle");
  const header = (
    <Space>
      <Button
        type="text"
        aria-label={t("common.back")}
        icon={<ArrowLeft size={16} />}
        onClick={() => navigate({ to: "/products" })}
      />
      <Breadcrumb
        items={[
          {
            title: t("catalog.products.title"),
            href: "/products",
            onClick: (event) => {
              event.preventDefault();
              navigate({ to: "/products" });
            },
          },
          { title: breadcrumbLabel },
        ]}
      />
    </Space>
  );

  if (isEdit && productPending) {
    return (
      <Card title={header}>
        <Skeleton active />
      </Card>
    );
  }

  const saving = createMutation.isPending || editMutation.isPending;

  return (
    <Card
      title={header}
      extra={
        <Button type="primary" loading={saving} onClick={() => form.submit()}>
          {t("common.save")}
        </Button>
      }
    >
      <Form<ProductFormValues>
        form={form}
        layout="vertical"
        initialValues={initialValues}
        onFinish={handleFinish}
      >
        <Tabs
          items={[
            {
              key: "general",
              label: t("catalog.products.tabs.general"),
              children: (
                <>
                  {locales.map((locale) => (
                    <Form.Item
                      key={`name-${locale}`}
                      name={["translations", locale, "name"]}
                      label={t("common.fieldWithLang", {
                        field: t("catalog.products.fields.name"),
                        lang: t(`lang.${locale}`),
                      })}
                      rules={[{ required: locale === me.shop.defaultLocale }]}
                    >
                      <Input />
                    </Form.Item>
                  ))}
                  {locales.map((locale) => (
                    <Form.Item
                      key={`description-${locale}`}
                      name={["translations", locale, "description"]}
                      label={t("common.fieldWithLang", {
                        field: t("catalog.products.fields.description"),
                        lang: t(`lang.${locale}`),
                      })}
                    >
                      <Input.TextArea rows={3} />
                    </Form.Item>
                  ))}
                  <Form.Item name="categoryId" label={t("catalog.products.fields.category")}>
                    <TreeSelect allowClear treeData={categoryOptions} treeDefaultExpandAll />
                  </Form.Item>
                  <Form.Item
                    name="unitId"
                    label={t("catalog.products.fields.unit")}
                    rules={[{ required: true }]}
                  >
                    <Select
                      options={(units ?? []).map((unit) => ({ value: unit.id, label: unit.name }))}
                    />
                  </Form.Item>
                  <Form.Item name="slug" label={t("catalog.products.fields.slug")}>
                    <Input />
                  </Form.Item>
                  <Form.Item name="sku" label={t("catalog.products.fields.sku")}>
                    <Input />
                  </Form.Item>
                  <Form.Item
                    name="isActive"
                    label={t("catalog.products.fields.isActive")}
                    valuePropName="checked"
                  >
                    <Switch />
                  </Form.Item>
                  <Form.Item
                    name="isFeatured"
                    label={t("catalog.products.fields.isFeatured")}
                    valuePropName="checked"
                  >
                    <Switch />
                  </Form.Item>
                </>
              ),
            },
            {
              key: "prices",
              label: t("catalog.products.tabs.prices"),
              children: (
                <>
                  <Form.Item
                    name="basePrice"
                    label={t("catalog.products.fields.basePrice")}
                    rules={[{ required: true }]}
                  >
                    <InputNumber min={0} precision={2} style={{ width: "100%" }} />
                  </Form.Item>
                  {showCostField && (
                    <Form.Item name="costPrice" label={t("catalog.products.fields.costPrice")}>
                      <InputNumber min={0} precision={2} style={{ width: "100%" }} />
                    </Form.Item>
                  )}
                  <Form.Item name="promoPrice" label={t("catalog.products.fields.promoPrice")}>
                    <InputNumber min={0} precision={2} style={{ width: "100%" }} />
                  </Form.Item>
                  <Form.Item name="promoRange" label={t("catalog.products.fields.promoRange")}>
                    <DatePicker.RangePicker showTime style={{ width: "100%" }} />
                  </Form.Item>
                </>
              ),
            },
            {
              key: "variants",
              label: t("catalog.products.tabs.variants"),
              disabled: !isEdit,
              children:
                isEdit && product ? (
                  <VariantsImagesTab product={product} />
                ) : (
                  <Card>{t("catalog.variants.saveFirst")}</Card>
                ),
            },
          ]}
        />
      </Form>
    </Card>
  );
}
