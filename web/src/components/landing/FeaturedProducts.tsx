import type { components } from "@savdo/api-client";

import type { Locale } from "../../lib/locale";
import { ProductCard } from "../ProductCard";

type PublicProductListItem = components["schemas"]["PublicProductListItem"];

/**
 * The single products grid on the redesigned homepage (D-121, Variant B —
 * the design canvas shows one "Tavsiya etilgan mahsulotlar" section, not
 * the old two-grid featured/newest split; see the task report for that
 * call). Renders nothing when `products` is empty, same "no data, no
 * section" rule the rest of this page follows.
 */
export function FeaturedProducts({
  id,
  title,
  products,
  locale,
  currency,
}: {
  id?: string;
  title: string;
  products: readonly PublicProductListItem[];
  locale: Locale;
  currency: string;
}) {
  if (products.length === 0) {
    return null;
  }
  return (
    <section id={id}>
      <h2 className="mb-5 font-bold text-2xl text-text tracking-tight sm:text-[28px]">{title}</h2>
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4">
        {products.map((product) => (
          <ProductCard key={product.id} product={product} locale={locale} currency={currency} />
        ))}
      </div>
    </section>
  );
}
