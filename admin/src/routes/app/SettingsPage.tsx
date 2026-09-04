import { locales } from "@savdo/i18n";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useRouter } from "@tanstack/react-router";
import { App, Button, Card, Form, Input, InputNumber, Select, Skeleton, Switch } from "antd";
import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { applyApiErrorToForm, notifyApiError } from "../../lib/errors";
import { fetchShop, type ShopPatch, updateShop } from "../../settings/api";

/** A short list of IANA timezones relevant to Savdo's shops — `Asia/Tashkent`
 * (the primary market) plus its regional neighbours. */
const TIMEZONES = [
  "Asia/Tashkent",
  "Asia/Samarkand",
  "Asia/Almaty",
  "Asia/Bishkek",
  "Asia/Dushanbe",
  "Europe/Moscow",
  "UTC",
];

interface SettingsFormValues {
  name: string;
  timezone: string;
  defaultLocale: (typeof locales)[number];
  allowNegativeStock: boolean;
  updateCostOnPurchase: boolean;
  lowStockThreshold: number;
}

export function SettingsPage() {
  const { t } = useTranslation();
  const { notification } = App.useApp();
  const queryClient = useQueryClient();
  const router = useRouter();
  const [form] = Form.useForm<SettingsFormValues>();

  const { data: shop, isPending } = useQuery({ queryKey: ["shop"], queryFn: fetchShop });

  useEffect(() => {
    if (shop) {
      form.setFieldsValue({
        name: shop.name,
        timezone: shop.timezone,
        defaultLocale: shop.defaultLocale,
        allowNegativeStock: shop.allowNegativeStock,
        updateCostOnPurchase: shop.updateCostOnPurchase,
        lowStockThreshold: shop.lowStockThreshold,
      });
    }
  }, [shop, form]);

  const saveMutation = useMutation({
    mutationFn: (values: SettingsFormValues) => {
      const body: ShopPatch = { ...values };
      return updateShop(body);
    },
    onSuccess: async (updated) => {
      queryClient.setQueryData(["shop"], updated);
      await queryClient.invalidateQueries({ queryKey: ["auth", "me"] });
      // The sider reads `me.shop.name` from a live `useMe()` subscription
      // (AppLayout), which the invalidate above already refreshes. Also
      // re-run `beforeLoad` so `authenticatedRoute`'s route context — the
      // `me` used for permission checks — is not left holding a stale
      // snapshot from before this save.
      await router.invalidate();
      notification.success({ title: t("settings.saved") });
    },
    onError: (error) => {
      if (!applyApiErrorToForm(form, error, t)) {
        notifyApiError(notification, error, t);
      }
    },
  });

  if (isPending || !shop) {
    return (
      <Card title={t("settings.title")}>
        <Skeleton active />
      </Card>
    );
  }

  return (
    <Card title={t("settings.title")}>
      <Form<SettingsFormValues>
        form={form}
        layout="vertical"
        style={{ maxWidth: 480 }}
        onFinish={(values) => saveMutation.mutate(values)}
      >
        <Form.Item label={t("settings.fields.slug")}>
          <Input value={shop.slug} disabled />
        </Form.Item>
        <Form.Item label={t("settings.fields.currency")}>
          <Input value={shop.currency} disabled />
        </Form.Item>
        <Form.Item name="name" label={t("settings.fields.name")} rules={[{ required: true }]}>
          <Input />
        </Form.Item>
        <Form.Item
          name="timezone"
          label={t("settings.fields.timezone")}
          rules={[{ required: true }]}
        >
          <Select options={TIMEZONES.map((timezone) => ({ value: timezone, label: timezone }))} />
        </Form.Item>
        <Form.Item
          name="defaultLocale"
          label={t("settings.fields.defaultLocale")}
          rules={[{ required: true }]}
        >
          <Select
            options={locales.map((locale) => ({ value: locale, label: t(`lang.${locale}`) }))}
          />
        </Form.Item>
        <Form.Item
          name="allowNegativeStock"
          label={t("settings.fields.allowNegativeStock")}
          valuePropName="checked"
        >
          <Switch />
        </Form.Item>
        <Form.Item
          name="updateCostOnPurchase"
          label={t("settings.fields.updateCostOnPurchase")}
          valuePropName="checked"
        >
          <Switch />
        </Form.Item>
        <Form.Item
          name="lowStockThreshold"
          label={t("settings.fields.lowStockThreshold")}
          rules={[{ required: true, type: "integer", min: 0 }]}
        >
          <InputNumber min={0} precision={0} style={{ width: "100%" }} />
        </Form.Item>
        <Form.Item>
          <Button type="primary" htmlType="submit" loading={saveMutation.isPending}>
            {t("settings.save")}
          </Button>
        </Form.Item>
      </Form>
    </Card>
  );
}
