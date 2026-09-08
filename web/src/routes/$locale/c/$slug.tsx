import type { components } from "@savdo/api-client";
import { createFileRoute, notFound } from "@tanstack/react-router";
import { useServerFn } from "@tanstack/react-start";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { ProductCard } from "../../../components/ProductCard";
import { listPublicCategories, listPublicProducts } from "../../../lib/publicApi.functions";
import { absoluteUrl, asRouteMatchHead, buildSeoHead, chooseOgImage } from "../../../lib/seo";

type PublicProductListItem = components["schemas"]["PublicProductListItem"];

export const Route = createFileRoute("/$locale/c/$slug")({
  loader: async ({ context: { locale }, params: { slug } }) => {
    const [categories, products] = await Promise.all([
      listPublicCategories({ data: { locale } }),
      listPublicProducts({ data: { locale, category: slug } }),
    ]);
    const category = categories.items.find((item) => item.slug === slug);
    if (!category) {
      throw notFound();
    }
    return { category, products };
  },
  head: ({ match, loaderData }) => {
    if (!loaderData) {
      return {};
    }
    const { shop, locale, siteUrl } = match.context;
    const { category } = loaderData;
    // D-99: category pages have no per-category description or image this
    // phase, so title/description/image all fall back to the shop's own.
    const title = `${category.name} — ${shop.name}`;
    const description = shop.blocks.seo?.description ?? shop.name;
    const image = chooseOgImage({
      siteUrl,
      heroImage: shop.blocks.hero?.image,
      fallbackUrl: absoluteUrl(siteUrl, "/og-default.png"),
    });
    return asRouteMatchHead(
      buildSeoHead({ siteUrl, locale, path: `/c/${category.slug}`, title, description, image }),
    );
  },
  component: CategoryPage,
});

function CategoryPage() {
  const { t } = useTranslation();
  const { locale, shop } = Route.useRouteContext();
  const { slug } = Route.useParams();
  const { category, products } = Route.useLoaderData();
  const loadMore = useServerFn(listPublicProducts);

  const [items, setItems] = useState<PublicProductListItem[]>(products.items);
  const [cursor, setCursor] = useState(products.nextCursor);
  const [loading, setLoading] = useState(false);

  const handleLoadMore = async () => {
    if (cursor == null) {
      return;
    }
    setLoading(true);
    try {
      const next = await loadMore({ data: { locale, category: slug, cursor } });
      setItems((prev) => [...prev, ...next.items]);
      setCursor(next.nextCursor);
    } finally {
      setLoading(false);
    }
  };

  return (
    <main className="mx-auto flex max-w-5xl flex-col gap-6 px-4 py-8">
      <h1 className="font-bold text-2xl text-text">{category.name}</h1>
      {items.length === 0 ? (
        <p className="rounded-md border border-bg border-dashed px-4 py-10 text-center text-muted">
          {t("web.category.empty")}
        </p>
      ) : (
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4">
          {items.map((product) => (
            <ProductCard
              key={product.id}
              product={product}
              locale={locale}
              currency={shop.currency}
            />
          ))}
        </div>
      )}
      {cursor != null && (
        <button
          type="button"
          onClick={handleLoadMore}
          disabled={loading}
          className="self-center rounded-md border border-primary px-4 py-2 text-primary disabled:opacity-50"
        >
          {t("common.loadMore")}
        </button>
      )}
    </main>
  );
}
