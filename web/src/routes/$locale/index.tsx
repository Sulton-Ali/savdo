import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import { CategoryCard } from "../../components/CategoryCard";
import { ContactsBlock } from "../../components/ContactsBlock";
import { HeroSection } from "../../components/HeroSection";
import { HoursTable } from "../../components/HoursTable";
import { ProductCard } from "../../components/ProductCard";
import { listPublicCategories, listPublicProducts } from "../../lib/publicApi.functions";

const FEATURED_LIMIT = 8;

export const Route = createFileRoute("/$locale/")({
  loader: async ({ context: { locale } }) => {
    const [categories, featured] = await Promise.all([
      listPublicCategories({ data: { locale } }),
      listPublicProducts({ data: { locale, featured: true, limit: FEATURED_LIMIT } }),
    ]);
    return { categories, featured };
  },
  head: ({ match }) => {
    const { shop } = match.context;
    const seo = shop.blocks.seo;
    return {
      meta: [
        { title: seo?.title ?? shop.name },
        { name: "description", content: seo?.description ?? shop.name },
      ],
    };
  },
  component: HomePage,
});

function HomePage() {
  const { t } = useTranslation();
  const { locale, shop } = Route.useRouteContext();
  const { categories, featured } = Route.useLoaderData();
  const blocks = shop.blocks;

  return (
    <main className="mx-auto flex max-w-5xl flex-col gap-10 px-4 py-8">
      <HeroSection hero={blocks.hero} fallbackTitle={shop.name} />

      {categories.items.length > 0 && (
        <section>
          <h2 className="mb-4 font-semibold text-text text-xl">{t("web.home.categoriesTitle")}</h2>
          <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4">
            {categories.items.map((category) => (
              <CategoryCard key={category.id} category={category} locale={locale} />
            ))}
          </div>
        </section>
      )}

      {featured.items.length > 0 && (
        <section>
          <h2 className="mb-4 font-semibold text-text text-xl">{t("web.home.featuredTitle")}</h2>
          <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4">
            {featured.items.map((product) => (
              <ProductCard key={product.id} product={product} locale={locale} />
            ))}
          </div>
        </section>
      )}

      {blocks.hours != null && (
        <section>
          <h2 className="mb-4 font-semibold text-text text-xl">{t("web.home.hoursTitle")}</h2>
          <HoursTable hours={blocks.hours} />
        </section>
      )}

      {blocks.contacts != null && (
        <section>
          <h2 className="mb-4 font-semibold text-text text-xl">{t("web.home.contactsTitle")}</h2>
          <ContactsBlock contacts={blocks.contacts} social={blocks.social} />
        </section>
      )}
    </main>
  );
}
