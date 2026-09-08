import type { Locale } from "@savdo/i18n";
import { Form, Input } from "antd";
import { useTranslation } from "react-i18next";

import type { ContentAbout, ContentResource } from "../../../lib/content";
import { ContentBlockCard } from "./ContentBlockCard";

interface AboutFormValues {
  title?: string;
  body?: string;
}

function aboutInitialValues(
  resource: ContentResource | undefined,
  locale: Locale,
): Partial<AboutFormValues> {
  const data = resource?.locales[locale]?.data as ContentAbout | undefined;
  return { title: data?.title, body: data?.body };
}

function buildAboutPayload(values: AboutFormValues): ContentAbout {
  const payload: ContentAbout = { body: values.body?.trim() ?? "" };
  const title = values.title?.trim();
  if (title) {
    payload.title = title;
  }
  return payload;
}

export function AboutCard({
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
    <ContentBlockCard<AboutFormValues, ContentAbout>
      contentKey="about"
      title={t("content.keys.about")}
      locale={locale}
      resource={resource}
      isPending={isPending}
      buildInitialValues={aboutInitialValues}
      buildPayload={buildAboutPayload}
    >
      {() => (
        <>
          <Form.Item name="title" label={t("content.about.title")}>
            <Input />
          </Form.Item>
          <Form.Item name="body" label={t("content.about.body")} rules={[{ required: true }]}>
            <Input.TextArea rows={5} />
          </Form.Item>
        </>
      )}
    </ContentBlockCard>
  );
}
