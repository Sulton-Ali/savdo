/**
 * The coral "about our family shop" card (D-121). Text-on-accent uses the
 * darker `landing-accent-hover` step (see `styles.css`'s TODO(T1) block),
 * not the base tone — this card's body copy is normal-size text, not a
 * large display heading, so it needs the ~4.64:1 pairing, not ~3.67:1.
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
      <span className="font-bold text-landing-on-accent/85 text-xs uppercase tracking-wide">
        {eyebrow}
      </span>
      <h2 className="font-extrabold text-2xl tracking-tight">{title}</h2>
      <p className="text-landing-on-accent/95 text-sm leading-relaxed">{body}</p>
    </div>
  );
}
