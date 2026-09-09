import { QuoteIcon } from "../icons";

/**
 * A sample-customer-quote card (D-121). Always marked `sampleLabel`
 * ("namuna"/"sample") — there is no real customer-quote data anywhere in
 * the product (no reviews/testimonials feature exists), so these are
 * placeholder copy the shop owner is meant to replace by hand later, never
 * presented as real feedback. `text-landing-accent` here is a large (28px)
 * decorative icon, not text, so the base tone (not `-hover`) is fine — see
 * `packages/ui-tokens/src/tokens.ts`'s `landing` doc comment.
 */
export function QuoteCard({
  quote,
  author,
  sampleLabel,
}: {
  quote: string;
  author: string;
  sampleLabel: string;
}) {
  return (
    <figure className="m-0 flex flex-col gap-3.5 rounded-landing-card bg-landing-surface p-6 shadow-landing-card">
      <QuoteIcon className="h-7 w-7 text-landing-accent" />
      <blockquote className="m-0 text-base text-text leading-relaxed">{quote}</blockquote>
      <figcaption className="flex items-center justify-between gap-2 text-muted text-sm">
        <span>{author}</span>
        {/* This pill's own background is bg-bg (#f5f5f4), not the card's
         * white — `text-muted` clears WCAG AA against that fill too (web's
         * `--color-muted` is #5b6472, see styles.css). */}
        <span className="rounded-full bg-bg px-2 py-0.5 font-semibold text-[11px] text-muted uppercase tracking-wide">
          {sampleLabel}
        </span>
      </figcaption>
    </figure>
  );
}
