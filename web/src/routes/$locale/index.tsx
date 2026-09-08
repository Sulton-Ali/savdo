import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import { CategoryGrid } from "../../components/CategoryGrid";
import { ContactsBlock } from "../../components/ContactsBlock";
import { HeroSection } from "../../components/HeroSection";
import { HoursTable } from "../../components/HoursTable";
import { ClockIcon, MapPinIcon } from "../../components/icons";
import { ProductCard } from "../../components/ProductCard";
import { listPublicCategories, listPublicProducts } from "../../lib/publicApi.functions";
import { absoluteUrl, asRouteMatchHead, buildSeoHead, chooseOgImage } from "../../lib/seo";

const PAGE_LIMIT = 8;

export const Route = createFileRoute("/$locale/")({
  loader: async ({ context: { locale } }) => {
    const [categories, featured, newest] = await Promise.all([
      listPublicCategories({ data: { locale } }),
      listPublicProducts({ data: { locale, featured: true, limit: PAGE_LIMIT } }),
      listPublicProducts({ data: { locale, limit: PAGE_LIMIT } }),
    ]);
    return { categories, featured, newest };
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
  const { categories, featured, newest } = Route.useLoaderData();
  const blocks = shop.blocks;

  return (
    <main className="mx-auto flex max-w-5xl flex-col gap-12 px-4 py-8">
      <HeroSection
        hero={blocks.hero}
        fallbackTitle={shop.name}
        ctaHref="#products"
        ctaLabel={t("web.hero.cta")}
      />

      <CategoryGrid categories={categories.items} locale={locale} />

      {featured.items.length > 0 && (
        <section>
          <h2 className="mb-4 font-semibold text-text text-xl">{t("web.home.featuredTitle")}</h2>
          <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4">
            {featured.items.map((product) => (
              <ProductCard
                key={product.id}
                product={product}
                locale={locale}
                currency={shop.currency}
              />
            ))}
          </div>
        </section>
      )}

      {newest.items.length > 0 && (
        <section id="products">
          <h2 className="mb-4 font-semibold text-text text-xl">{t("web.home.newProductsTitle")}</h2>
          <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4">
            {newest.items.map((product) => (
              <ProductCard
                key={product.id}
                product={product}
                locale={locale}
                currency={shop.currency}
              />
            ))}
          </div>
        </section>
      )}

      {blocks.hours != null && (
        <section>
          <h2 className="mb-4 flex items-center gap-2 font-semibold text-text text-xl">
            <ClockIcon className="h-5 w-5 text-primary" />
            {t("web.home.hoursTitle")}
          </h2>
          <HoursTable hours={blocks.hours} />
        </section>
      )}

      {blocks.contacts != null && (
        <section>
          <h2 className="mb-4 flex items-center gap-2 font-semibold text-text text-xl">
            <MapPinIcon className="h-5 w-5 text-primary" />
            {t("web.home.contactsTitle")}
          </h2>
          <ContactsBlock contacts={blocks.contacts} social={blocks.social} />
        </section>
      )}
    </main>
  );
}
