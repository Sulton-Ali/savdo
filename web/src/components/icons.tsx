import type { ReactNode } from "react";

/**
 * Small hand-drawn inline SVG icons (deliverable 4 — hours/contacts need
 * icons; `lucide-react` is not a `web/` dependency, D-25/`02-TECH-STACK.md`
 * only list it for `admin`/`mobile`, and adding it here is a new
 * dependency outside this task's approval). Decorative only
 * (`aria-hidden="true"`, literal on the shared `Icon` wrapper so Biome's
 * `a11y/noSvgWithoutTitle` rule sees it) — every link that carries one
 * already has its own `aria-label`.
 */
type IconProps = { className?: string };

function Icon({ className = "h-4 w-4", children }: IconProps & { children: ReactNode }) {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={2}
      strokeLinecap="round"
      strokeLinejoin="round"
      className={className}
      aria-hidden="true"
    >
      {children}
    </svg>
  );
}

export function PhoneIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <path d="M6.6 3h3.2l1.2 4.4-2.1 1.7a13 13 0 0 0 6 6l1.7-2.1 4.4 1.2v3.2a2 2 0 0 1-2.2 2A17 17 0 0 1 4.6 5.2 2 2 0 0 1 6.6 3z" />
    </Icon>
  );
}

export function MapPinIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <path d="M12 21s7-6.7 7-12a7 7 0 1 0-14 0c0 5.3 7 12 7 12z" />
      <circle cx="12" cy="9" r="2.5" />
    </Icon>
  );
}

export function ClockIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <circle cx="12" cy="12" r="9" />
      <path d="M12 7v5l3.2 2" />
    </Icon>
  );
}

export function TelegramIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <path d="M3 11.5 20.5 4l-3 16-6-4.6-3 3-.5-5z" />
      <path d="M11.5 14.4 20.5 4" />
    </Icon>
  );
}

export function InstagramIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <rect x="3.5" y="3.5" width="17" height="17" rx="5" />
      <circle cx="12" cy="12" r="4.2" />
      <circle cx="17" cy="7" r="0.6" fill="currentColor" stroke="none" />
    </Icon>
  );
}

/** Decorative "family" glyph for the hero's placeholder photo block (D-121,
 * Variant B — the shop has not uploaded a real photo yet). */
export function FamilyIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <circle cx="7" cy="7" r="2.3" />
      <path d="M2.5 19v-3a4.3 4.3 0 0 1 8.6 0v3" />
      <circle cx="17" cy="7" r="2.3" />
      <path d="M12.5 19v-3a4.3 4.3 0 0 1 8.6 0v3" />
      <circle cx="12" cy="13.5" r="1.6" />
      <path d="M9 22v-2.5a3 3 0 0 1 6 0V22" />
    </Icon>
  );
}

/** Decorative "clothes hanger" glyph — the shared placeholder for a
 * category tile or product photo with no real image yet (D-121). One glyph
 * for every category, whatever the shop names it: nothing in the public
 * category data (`PublicCategory`) says "this is kidswear" for the icon to
 * key off. */
export function HangerIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <path d="M7.5 3.75l2.25-1.5c1.125 1.5 3.375 1.5 4.5 0l2.25 1.5 3.75 3-2.25 3-1.5-1.5V21H7.5V8.25l-1.5 1.5-2.25-3z" />
    </Icon>
  );
}

/** Teal "go" arrow for a category row card's trailing badge (D-121). */
export function ArrowRightIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <path d="M5 12h14" />
      <path d="M13 6l6 6-6 6" />
    </Icon>
  );
}

/** Filled quote glyph for a customer-quote card (D-121) — the one icon in
 * this file that is a solid shape, matching the design canvas exactly,
 * rather than the shared stroked `Icon` wrapper. */
export function QuoteIcon({ className = "h-6 w-6" }: IconProps) {
  return (
    <svg viewBox="0 0 24 24" fill="currentColor" className={className} aria-hidden="true">
      <path d="M4 12h5v7H3v-6c0-3.9 2-6.6 5.5-7.6l.7 1.8C6.8 8 5.6 9.3 5.2 11A3 3 0 0 0 4 12zm10 0h5v7h-6v-6c0-3.9 2-6.6 5.5-7.6l.7 1.8c-2.4.8-3.6 2.1-4 3.8a3 3 0 0 0-1.2 1z" />
    </svg>
  );
}

/** Arrow-out-of-box glyph for the "view on map" link (D-121). */
export function ExternalLinkIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <path d="M14 4h6v6" />
      <path d="M20 4 10 14" />
      <path d="M18 13v6H5V6h6" />
    </Icon>
  );
}
