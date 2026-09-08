import type { Locale } from "@savdo/i18n";
import { Form, Input } from "antd";
import { useTranslation } from "react-i18next";

import type { ContentResource, ContentSocial } from "../../../lib/content";
import { ContentBlockCard } from "./ContentBlockCard";

interface SocialFormValues {
  telegram?: string;
  instagram?: string;
}

function socialInitialValues(
  resource: ContentResource | undefined,
  locale: Locale,
): Partial<SocialFormValues> {
  const data = resource?.locales[locale]?.data as ContentSocial | undefined;
  return { telegram: data?.telegram, instagram: data?.instagram };
}

function buildSocialPayload(values: SocialFormValues): ContentSocial {
  const payload: ContentSocial = {};
  const telegram = values.telegram?.trim();
  if (telegram) {
    payload.telegram = telegram;
  }
  const instagram = values.instagram?.trim();
  if (instagram) {
    payload.instagram = instagram;
  }
  return payload;
}

export function SocialCard({
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
    <ContentBlockCard<SocialFormValues, ContentSocial>
      contentKey="social"
      title={t("content.keys.social")}
      locale={locale}
      resource={resource}
      isPending={isPending}
      buildInitialValues={socialInitialValues}
      buildPayload={buildSocialPayload}
    >
      {() => (
        <>
          <Form.Item name="telegram" label={t("content.social.telegram")}>
            <Input />
          </Form.Item>
          <Form.Item name="instagram" label={t("content.social.instagram")}>
            <Input />
          </Form.Item>
        </>
      )}
    </ContentBlockCard>
  );
}
