import { TelegramButton } from "./TelegramButton";

/**
 * The closing "ask on Telegram" card before the footer (D-121) — a teal top
 * border (decorative, non-text, so the base `landing-secondary` tone is
 * fine) and the same `TelegramButton` as the hero. Hidden entirely when
 * `telegramHref` is `null` (no safe `https://t.me/...` link configured):
 * a CTA with nowhere to go is worse than no CTA, and every other
 * conditional block on this page already follows "no data, no section".
 */
export function TelegramCtaSection({
  telegramHref,
  telegramLabel,
  heading,
  subheading,
}: {
  telegramHref: string | null;
  telegramLabel: string;
  heading: string;
  subheading: string;
}) {
  if (telegramHref == null) {
    return null;
  }
  return (
    <div className="flex flex-col items-stretch gap-4 rounded-landing-card border-landing-secondary border-t-4 bg-landing-surface p-6 shadow-landing-card sm:flex-row sm:items-center sm:justify-between">
      <div className="flex flex-col gap-1">
        <span className="font-bold text-lg text-text sm:text-xl">{heading}</span>
        <span className="text-muted text-sm">{subheading}</span>
      </div>
      <TelegramButton href={telegramHref} label={telegramLabel} />
    </div>
  );
}
