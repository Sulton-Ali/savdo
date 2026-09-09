import { AboutCard } from "./AboutCard";
import { QuoteCard } from "./QuoteCard";

/** One customer quote — always sample copy, see `QuoteCard`'s doc comment. */
export interface SampleQuote {
  text: string;
  author: string;
}

/**
 * "About our family shop" + sample quotes (D-121): the about card (real
 * `ContentAbout` when the shop has set one, else an i18n placeholder — the
 * caller resolves that fallback, see `$locale/index.tsx`) sits beside two
 * fixed sample quote cards. Always renders (unlike the other conditional
 * sections): the about card's own i18n placeholder means there is always
 * something to show, and the quotes are explicitly sample copy, not data
 * that could be "empty".
 */
export function AboutSection({
  eyebrow,
  title,
  body,
  quotes,
  sampleLabel,
}: {
  eyebrow: string;
  title: string;
  body: string;
  quotes: readonly SampleQuote[];
  sampleLabel: string;
}) {
  return (
    <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
      <AboutCard eyebrow={eyebrow} title={title} body={body} />
      {/* `quote.author` is placeholder sample copy ("[Mijoz ismi]") and can
       * repeat across entries, so it is not a safe React key on its own
       * (review) — `quote.text` is unique per entry and doubles as one. */}
      {quotes.map((quote) => (
        <QuoteCard
          key={quote.text}
          quote={quote.text}
          author={quote.author}
          sampleLabel={sampleLabel}
        />
      ))}
    </div>
  );
}
