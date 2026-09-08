import type { Locale } from "@savdo/i18n";
import { Form, Input } from "antd";
import { useTranslation } from "react-i18next";

import type { ContentResource, ContentSeo } from "../../../lib/content";
import { ContentBlockCard } from "./ContentBlockCard";

interface SeoFormValues {
  title?: string;
  description?: string;
}

function seoInitialValues(
  resource: ContentResource | undefined,
  locale: Locale,
): Partial<SeoFormValues> {
  const data = resource?.locales[locale]?.data as ContentSeo | undefined;
  return { title: data?.title, description: data?.description };
}

function buildSeoPayload(values: SeoFormValues): ContentSeo {
  return {
    title: values.title?.trim() ?? "",
    description: values.description?.trim() ?? "",
  };
}

export function SeoCard({
  locale,
  resource,
  isPending,
}: {
  locale: Locale;
  resource: ContentResource | undefined;
  isPending: boolean;
}) {
  const { t } = useTranslation();
  return (
    <ContentBlockCard<SeoFormValues, ContentSeo>
      contentKey="seo"
      title={t("content.keys.seo")}
      locale={locale}
      resource={resource}
      isPending={isPending}
      buildInitialValues={seoInitialValues}
      buildPayload={buildSeoPayload}
    >
      {() => (
        <>
          <Form.Item name="title" label={t("content.seo.title")} rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item
            name="description"
            label={t("content.seo.description")}
            rules={[{ required: true }]}
          >
            <Input.TextArea rows={3} />
          </Form.Item>
        </>
      )}
    </ContentBlockCard>
  );
}
