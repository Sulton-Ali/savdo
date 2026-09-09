import { TelegramIcon } from "../icons";

/**
 * The primary "chat on Telegram" CTA (D-121), reused by the hero, the
 * closing CTA card before the footer, and (phase-7.5 T4) the header. A
 * plain `<a>` to an external link — same reasoning as `LanguageSwitcher`'s
 * doc comment: `href` is a computed URL (`ContentSocial.telegram`, already
 * checked with `isSafeHttpsUrl` by the caller), never a typed router
 * destination.
 *
 * Filled with `landing-accent-hover` (the darker step), not the base
 * `landing-accent`: white text on the base tone measures ~3.67:1 (WCAG AA
 * passes only large/bold text), the darker step ~4.64:1 (passes normal
 * text too) — see `packages/ui-tokens/src/tokens.ts`'s `landing` doc
 * comment. `hover:brightness-95` gives a visible interactive state without
 * a third colour token.
 *
 * `responsive` (phase-7.5 T4, the header's own use per the design canvas's
 * `BMobile`/`Components` artboards): below `sm` the button shrinks to a
 * 44px icon-only circle (the mobile touch-target floor) and the label is
 * `sr-only` — still in the accessible name, just not painted — expanding
 * back to the full icon+label pill at `sm` and up. One `<a>`, not two
 * swapped by breakpoint, so there is only ever one Telegram link in the
 * header's accessibility tree and tab order.
 */
export function TelegramButton({
  href,
  label,
  className = "",
  responsive = false,
}: {
  href: string;
  label: string;
  className?: string;
  responsive?: boolean;
}) {
  const shapeClass = responsive
    ? "h-11 w-11 rounded-full sm:h-auto sm:w-auto sm:rounded-xl sm:px-6 sm:py-3"
    : "rounded-xl px-6 py-3";
  return (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      className={`inline-flex items-center justify-center gap-2 bg-landing-accent-hover font-semibold text-base text-landing-on-accent transition hover:brightness-95 ${shapeClass} ${className}`}
    >
      <TelegramIcon className="h-5 w-5 shrink-0" />
      <span className={responsive ? "sr-only sm:not-sr-only" : undefined}>{label}</span>
    </a>
  );
}
