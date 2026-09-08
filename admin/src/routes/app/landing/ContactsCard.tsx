import type { Locale } from "@savdo/i18n";
import { Form, Input } from "antd";
import { useTranslation } from "react-i18next";

import type { ContentContacts, ContentResource } from "../../../lib/content";
import { ContentBlockCard } from "./ContentBlockCard";

interface ContactsFormValues {
  phone?: string;
  address?: string;
  mapUrl?: string;
}

function contactsInitialValues(
  resource: ContentResource | undefined,
  locale: Locale,
): Partial<ContactsFormValues> {
  const data = resource?.locales[locale]?.data as ContentContacts | undefined;
  return { phone: data?.phone, address: data?.address, mapUrl: data?.mapUrl };
}

function buildContactsPayload(values: ContactsFormValues): ContentContacts {
  const payload: ContentContacts = {
    phone: values.phone?.trim() ?? "",
    address: values.address?.trim() ?? "",
  };
  const mapUrl = values.mapUrl?.trim();
  if (mapUrl) {
    payload.mapUrl = mapUrl;
  }
  return payload;
}

export function ContactsCard({
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
    <ContentBlockCard<ContactsFormValues, ContentContacts>
      contentKey="contacts"
      title={t("content.keys.contacts")}
      locale={locale}
      resource={resource}
      isPending={isPending}
      buildInitialValues={contactsInitialValues}
      buildPayload={buildContactsPayload}
    >
      {() => (
        <>
          <Form.Item name="phone" label={t("content.contacts.phone")} rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item
            name="address"
            label={t("content.contacts.address")}
            rules={[{ required: true }]}
          >
            <Input />
          </Form.Item>
          <Form.Item
            name="mapUrl"
            label={t("content.contacts.mapUrl")}
            extra={t("content.contacts.mapUrlHint")}
          >
            <Input />
          </Form.Item>
        </>
      )}
    </ContentBlockCard>
  );
}
