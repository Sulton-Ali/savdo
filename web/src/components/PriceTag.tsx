import { displayPrice, type PriceLike } from "../lib/money";

/** Current price bold, the struck-through regular price (only while a
 * promo is active) on its own line below, smaller and muted — stacked
 * rather than inline so neither ever wraps mid-number in a narrow product
 * card (D-103). */
export function PriceTag({ price, currency }: { price: PriceLike; currency: string }) {
  const { current, strikethrough } = displayPrice(price, currency);
  return (
    <span className="flex flex-col">
      <span className="font-semibold text-text">{current}</span>
      {strikethrough != null && (
        <span className="text-muted text-xs line-through">{strikethrough}</span>
      )}
    </span>
  );
}
