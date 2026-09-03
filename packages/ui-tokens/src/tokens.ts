/**
 * Shared design tokens for Savdo's TypeScript apps (admin, web, mobile).
 * Single source of truth for colour, radius and font — consumed as a typed
 * object (e.g. by Ant Design's `ConfigProvider` in `admin/`) and mirrored as
 * CSS variables in `./tokens.css` for plain CSS / Tailwind usage.
 *
 * `color.primary` is a placeholder (`#0f766e`, teal-700) chosen only so the
 * apps have a working theme from Phase 0. The real brand colour is an owner
 * decision for a later phase — swap it here and in `tokens.css` when picked.
 */
export const tokens = {
  color: {
    primary: "#0f766e",
    primaryHover: "#0d9488",
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
    family: "Inter, system-ui, sans-serif",
  },
} as const;

export type Tokens = typeof tokens;
