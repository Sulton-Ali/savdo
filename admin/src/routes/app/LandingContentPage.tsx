import { locales } from "@savdo/i18n";
import { Card, Space, Tabs } from "antd";
import { useTranslation } from "react-i18next";

import { useContentResource } from "../../lib/content";
import { AboutCard } from "./landing/AboutCard";
import { ContactsCard } from "./landing/ContactsCard";
import { HeroCard } from "./landing/HeroCard";
import { HoursCard } from "./landing/HoursCard";
import { SeoCard } from "./landing/SeoCard";
import { SocialCard } from "./landing/SocialCard";

/** One locale tab's worth of cards. A separate instance mounts per locale
 * (see the `Tabs` below) so each tab holds its own `Form` state; every
 * instance shares the same six `["content", key]` query cache entries
 * (`lib/content.ts`), so switching tabs never re-fetches. */
function LandingContentTab({ locale }: { locale: (typeof locales)[number] }) {
  const hero = useContentResource("hero");
  const about = useContentResource("about");
  const hours = useContentResource("hours");
  const contacts = useContentResource("contacts");
  const social = useContentResource("social");
  const seo = useContentResource("seo");

  return (
    <Space direction="vertical" size="middle" style={{ width: "100%" }}>
      <HeroCard locale={locale} resource={hero.data} isPending={hero.isPending} />
      <AboutCard locale={locale} resource={about.data} isPending={about.isPending} />
      <HoursCard locale={locale} resource={hours.data} isPending={hours.isPending} />
      <ContactsCard locale={locale} resource={contacts.data} isPending={contacts.isPending} />
      <SocialCard locale={locale} resource={social.data} isPending={social.isPending} />
      <SeoCard locale={locale} resource={seo.data} isPending={seo.isPending} />
    </Space>
  );
}

/**
 * `/settings/landing` (T4 spec, D-99/O-19/O-21): a manager or owner edits
 * the six landing content blocks per locale. Gated by `content.manage` at
 * the route level (`landingContentRoute.tsx`), same pattern as `/settings`
 * and `/staff`.
 */
export function LandingContentPage() {
  const { t } = useTranslation();

  return (
    <Card title={t("content.title")}>
      <Tabs
        items={locales.map((locale) => ({
          key: locale,
          label: t(`lang.${locale}`),
          children: <LandingContentTab locale={locale} />,
        }))}
      />
    </Card>
  );
}
