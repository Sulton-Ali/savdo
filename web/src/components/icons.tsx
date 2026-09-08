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
