import { createFileRoute } from "@tanstack/react-router";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { AvailabilityBadge } from "../../../components/AvailabilityBadge";
import { CoverImage } from "../../../components/CoverImage";
import { PriceTag } from "../../../components/PriceTag";
import { getPublicProductBySlug } from "../../../lib/publicApi.functions";
import { absoluteUrl, asRouteMatchHead, buildSeoHead, chooseOgImage } from "../../../lib/seo";
import { splitParagraphs } from "../../../lib/text";

export const Route = createFileRoute("/$locale/p/$slug")({
  loader: async ({ context: { locale }, params: { slug } }) =>
    getPublicProductBySlug({ data: { locale, slug } }),
  head: ({ match, loaderData }) => {
    if (!loaderData) {
      return {};
    }
    const { shop, locale, siteUrl } = match.context;
    const product = loaderData;
    const title = `${product.name} — ${shop.name}`;
    const description =
      product.description != null && product.description !== ""
        ? product.description.slice(0, 155)
        : (shop.blocks.seo?.description ?? shop.name);
    // O-18: the product's own cover image, else the shop's hero photo, else the static brand image.
    const image = chooseOgImage({
      siteUrl,
      productImages: product.images,
      heroImage: shop.blocks.hero?.image,
      fallbackUrl: absoluteUrl(siteUrl, "/og-default.png"),
    });
    return asRouteMatchHead(
      buildSeoHead({ siteUrl, locale, path: `/p/${product.slug}`, title, description, image }),
    );
  },
  component: ProductPage,
});

function ProductPage() {
  const { t } = useTranslation();
  const { shop } = Route.useRouteContext();
  const product = Route.useLoaderData();
  const images = product.images ?? [];
  const [activeImage, setActiveImage] = useState(0);
  const main = images[activeImage] ?? images[0];

  return (
    <main className="mx-auto flex max-w-5xl flex-col gap-8 px-4 py-8 sm:grid sm:grid-cols-2 sm:items-start">
      <div className="flex flex-col gap-2">
        <CoverImage image={main?.urls} size="full" alt={product.name} loading="eager" />
        {images.length > 1 && (
          <div className="flex gap-2">
            {images.map((image, index) => (
              <button
                key={image.id}
                type="button"
                onClick={() => setActiveImage(index)}
                aria-label={t("web.product.galleryImageAlt", { name: product.name })}
                className={`overflow-hidden rounded-md ${index === activeImage ? "ring-2 ring-primary" : ""}`}
              >
                <CoverImage image={image.urls} size="thumb" alt="" className="w-16" />
              </button>
            ))}
          </div>
        )}
      </div>

      <div className="flex flex-col gap-4">
        <h1 className="font-bold text-2xl text-text">{product.name}</h1>

        {product.description != null && product.description !== "" && (
          <div className="flex flex-col gap-2 text-text">
            {splitParagraphs(product.description).map((paragraph, index) => (
              // biome-ignore lint/suspicious/noArrayIndexKey: a static, never-reordered list of paragraphs split from one string (per review).
              <p key={index} className="whitespace-pre-line">
                {paragraph}
              </p>
            ))}
          </div>
        )}

        {product.variants != null && product.variants.length > 0 && (
          <div>
            <h2 className="mb-2 font-semibold text-lg text-text">
              {t("web.product.variantsTitle")}
            </h2>
            <ul className="flex flex-col gap-3">
              {product.variants.map((variant) => (
                <li
                  key={variant.id}
                  className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-bg p-3"
                >
                  <span className="text-text">
                    {Object.entries(variant.attributes)
                      .map(
                        ([code, value]) =>
                          `${code.charAt(0).toUpperCase()}${code.slice(1)}: ${value}`,
                      )
                      .join(", ")}
                  </span>
                  <PriceTag price={variant.price} currency={shop.currency} />
                  <AvailabilityBadge value={variant.availability} />
                </li>
              ))}
            </ul>
          </div>
        )}
      </div>
    </main>
  );
}
