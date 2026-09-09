import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import { CategoryGrid } from "../../components/CategoryGrid";
import { AboutSection, type SampleQuote } from "../../components/landing/AboutSection";
import { ContactsCard } from "../../components/landing/ContactsCard";
import { FeaturedProducts } from "../../components/landing/FeaturedProducts";
import { HeroCard } from "../../components/landing/HeroCard";
import { HoursCard } from "../../components/landing/HoursCard";
import { TelegramCtaSection } from "../../components/landing/TelegramCtaSection";
import { listPublicCategories, listPublicProducts } from "../../lib/publicApi.functions";
import { absoluteUrl, asRouteMatchHead, buildSeoHead, chooseOgImage } from "../../lib/seo";
import { splitParagraphs } from "../../lib/text";
import { isSafeHttpsUrl } from "../../lib/url";

const PAGE_LIMIT = 8;

export const Route = createFileRoute("/$locale/")({
  loader: async ({ context: { locale } }) => {
    const [categories, featured] = await Promise.all([
      listPublicCategories({ data: { locale } }),
      listPublicProducts({ data: { locale, featured: true, limit: PAGE_LIMIT } }),
    ]);
    return { categories, featured };
  },
  head: ({ match }) => {
    const { shop, locale, siteUrl } = match.context;
    const seo = shop.blocks.seo;
    const title = seo?.title ?? shop.name;
    const description = seo?.description ?? shop.name;
    const image = chooseOgImage({
      siteUrl,
      heroImage: shop.blocks.hero?.image,
      fallbackUrl: absoluteUrl(siteUrl, "/og-default.png"),
    });
    return asRouteMatchHead(buildSeoHead({ siteUrl, locale, path: "", title, description, image }));
  },
  component: HomePage,
});

function HomePage() {
  const { t } = useTranslation();
  const { locale, shop } = Route.useRouteContext();
  const { categories, featured } = Route.useLoaderData();
  const blocks = shop.blocks;
  const about = blocks.about;

  const telegramHref =
    blocks.social?.telegram != null && isSafeHttpsUrl(blocks.social.telegram)
      ? blocks.social.telegram
      : null;

  const aboutBody =
    about?.body != null && about.body !== ""
      ? (splitParagraphs(about.body)[0] ?? t("web.home.aboutPlaceholder"))
      : t("web.home.aboutPlaceholder");

  const quotes: SampleQuote[] = [
    { text: t("web.home.quote1Text"), author: t("web.home.quote1Author") },
    { text: t("web.home.quote2Text"), author: t("web.home.quote2Author") },
  ];

  return (
    <main className="mx-auto flex max-w-5xl flex-col gap-8 bg-bg px-4 py-6 sm:gap-10 sm:px-6 sm:py-10">
      <HeroCard
        hero={blocks.hero}
        fallbackTitle={shop.name}
        eyebrow={t("web.hero.eyebrow")}
        telegramHref={telegramHref}
        telegramLabel={t("web.telegramCta")}
        catalogLabel={t("web.hero.cta")}
        catalogHref="#products"
        photoLabel={t("web.hero.photoPlaceholder")}
      />

      <CategoryGrid categories={categories.items} locale={locale} />

      <FeaturedProducts
        id="products"
        title={t("web.home.featuredTitle")}
        products={featured.items}
        locale={locale}
        currency={shop.currency}
      />

      {(blocks.hours != null || blocks.contacts != null) && (
        <div className="grid gap-4 lg:grid-cols-2">
          {blocks.hours != null && (
            <HoursCard hours={blocks.hours} title={t("web.home.hoursTitle")} />
          )}
          {blocks.contacts != null && (
            <ContactsCard
              contacts={blocks.contacts}
              social={blocks.social}
              title={t("web.home.contactsTitle")}
            />
          )}
        </div>
      )}

      <AboutSection
        eyebrow={t("web.home.aboutEyebrow")}
        title={about?.title ?? t("web.home.aboutTitle")}
        body={aboutBody}
        quotes={quotes}
        sampleLabel={t("web.home.sampleLabel")}
      />

      <TelegramCtaSection
        telegramHref={telegramHref}
        telegramLabel={t("web.telegramCta")}
        heading={t("web.home.ctaHeading")}
        subheading={t("web.home.ctaSubheading")}
      />
    </main>
  );
}
