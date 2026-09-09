/**
 * The coral "about our family shop" card (D-121). Text-on-accent uses the
 * darker `landing-accent-hover` step, not the base tone — this card's body
 * copy is normal-size text, not a large display heading, so it needs the
 * ~4.64:1 pairing, not ~3.67:1 (see `packages/ui-tokens/src/tokens.ts`'s
 * `landing` doc comment for the full contrast table). That ratio is
 * measured against solid `landing.onAccent` — an `/85`/`/95` opacity
 * modifier blends the text toward the card's own fill and drops it back
 * under 4.5:1 (review), so every line here stays at full opacity; the
 * eyebrow/heading/body hierarchy comes from size and weight only.
 */
export function AboutCard({
  eyebrow,
  title,
  body,
}: {
  eyebrow: string;
  title: string;
  body: string;
}) {
  return (
    <div className="flex flex-col gap-3 rounded-landing-card bg-landing-accent-hover p-6 text-landing-on-accent sm:p-7">
      <span className="font-bold text-landing-on-accent text-xs uppercase tracking-wide">
        {eyebrow}
      </span>
      <h2 className="font-extrabold text-2xl tracking-tight">{title}</h2>
      <p className="text-landing-on-accent text-sm leading-relaxed">{body}</p>
    </div>
  );
}
