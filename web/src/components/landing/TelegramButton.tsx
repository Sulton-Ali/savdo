import { TelegramIcon } from "../icons";

/**
 * The primary "chat on Telegram" CTA (D-121), reused by the hero and the
 * closing CTA card before the footer. A plain `<a>` to an external link —
 * same reasoning as `LanguageSwitcher`'s doc comment: `href` is a computed
 * URL (`ContentSocial.telegram`, already checked with `isSafeHttpsUrl` by
 * the caller), never a typed router destination.
 *
 * Filled with `landing-accent-hover` (the darker step), not the base
 * `landing-accent`: white text on the base tone measures ~3.67:1 (WCAG AA
 * passes only large/bold text), the darker step ~4.64:1 (passes normal
 * text too) — see `styles.css`'s TODO(T1) block. `hover:brightness-95`
 * gives a visible interactive state without a third colour token.
 */
export function TelegramButton({
  href,
  label,
  className = "",
}: {
  href: string;
  label: string;
  className?: string;
}) {
  return (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      className={`inline-flex items-center justify-center gap-2 rounded-xl bg-landing-accent-hover px-6 py-3 font-semibold text-base text-landing-on-accent transition hover:brightness-95 ${className}`}
    >
      <TelegramIcon className="h-5 w-5" />
      {label}
    </a>
  );
}
