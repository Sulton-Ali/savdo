/**
 * Shared design tokens for Savdo's TypeScript apps (admin, web, mobile).
 * Single source of truth for colour, radius and font — consumed as a typed
 * object (e.g. by Ant Design's `ConfigProvider` in `admin/`) and mirrored as
 * CSS variables in `./tokens.css` for plain CSS / Tailwind usage.
 *
 * `color.primary` is terracotta (`#c2410c`) — the owner's brand pick (D-36).
 * Provisional: may change after visual review in Phase 6 — swap it here and
 * in `tokens.css` if it does.
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
} as const;

export type Tokens = typeof tokens;
