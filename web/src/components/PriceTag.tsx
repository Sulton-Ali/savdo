import { displayPrice, type PriceLike } from "../lib/money";

export function PriceTag({ price, currency }: { price: PriceLike; currency: string }) {
  const { current, strikethrough } = displayPrice(price, currency);
  return (
    <span className="flex items-baseline gap-2">
      <span className="font-semibold text-text">{current}</span>
      {strikethrough != null && (
        <span className="text-muted text-sm line-through">{strikethrough}</span>
      )}
    </span>
  );
}
