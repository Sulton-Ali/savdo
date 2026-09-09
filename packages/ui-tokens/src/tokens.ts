/**
 * Shared design tokens for Savdo's TypeScript apps (admin, web, mobile).
 * Single source of truth for colour, radius and font — consumed as a typed
 * object (e.g. by Ant Design's `ConfigProvider` in `admin/`) and mirrored as
 * CSS variables in `./tokens.css` for plain CSS / Tailwind usage.
 *
 * `color.primary` is terracotta (`#c2410c`) — the owner's brand pick (D-36).
 * Provisional: may change after visual review in Phase 6 — swap it here and
 * in `tokens.css` if it does.
 *
 * `landing.*` (D-121) are scoped to the public landing's Variant B "Warm
 * cards" redesign only — they do not replace `color.primary` (D-36
 * terracotta stays the shared admin/mobile brand token).
 *
 * WCAG AA contrast (4.5:1 normal text, 3:1 large text/UI), computed against
 * white (`#ffffff`), relative luminance per WCAG 2.x:
 *   - white on `landing.accent`       (#e05a47): 3.67:1 — large text/UI only
 *   - white on `landing.accentHover`  (#c94a39): 4.64:1 — passes normal text
 *   - white on `landing.secondary`    (#1f8a80): 4.20:1 — large text/UI only
 *   - white on `landing.secondaryHover` (#176f67): 5.99:1 — passes normal text
 * Both base accent/secondary fall short of the 4.5:1 normal-text threshold
 * (not just accent), so normal-size text on either must use the hover/darker
 * step; the base tones are for large text (≥ 24px / 19px bold), icons and
 * non-text UI elements only. See `tokens.test.ts` for the same ratios
 * asserted in code.
 *
 * `landing.shadow.card` is D-121's `rgba(31,41,55,.08)` spec, written with
 * standard CSS spacing and a leading zero — same colour and alpha.
 */
export const tokens = {
  color: {
    primary: "#c2410c",
    primaryHover: "#9a3412",
    bg: "#f5f5f4",
    surface: "#ffffff",
    text: "#1f2937",
    muted: "#6b7280",
    success: "#16a34a",
    warning: "#d97706",
    danger: "#dc2626",
  },
  radius: {
    sm: 4,
    md: 8,
    lg: 12,
  },
  font: {
    family: '"Inter Variable", Inter, system-ui, sans-serif',
  },
  landing: {
    accent: "#e05a47",
    accentHover: "#c94a39",
    onAccent: "#ffffff",
    secondary: "#1f8a80",
    secondaryHover: "#176f67",
    onSecondary: "#ffffff",
    surface: "#ffffff",
    radius: {
      card: 16,
    },
    shadow: {
      card: "0 4px 16px rgba(31, 41, 55, 0.08)",
    },
  },
} as const;

export type Tokens = typeof tokens;
