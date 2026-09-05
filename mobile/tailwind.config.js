const { tokens } = require("@savdo/ui-tokens");

/**
 * Colours below come straight from `@savdo/ui-tokens` (D-25) — Node 24's
 * native TypeScript support and synchronous `require(esm)` let this CJS
 * config `require()` the package's `.ts` source directly, so there's
 * nothing to keep in sync by hand here (unlike `tokens.css`, which has no
 * build step of its own). Only genuinely non-brand UI chrome — `border`,
 * `input`, `secondary`, `accent` — stays as a literal: neutral tones picked
 * to read well against `background`/`card`, not part of the brand palette.
 * These semantic names match the copied react-native-reusables components
 * in `src/components/ui`.
 */
const savdo = {
  primary: tokens.color.primary,
  primaryForeground: tokens.color.surface,
  background: tokens.color.surface,
  foreground: tokens.color.text,
  card: tokens.color.surface,
  cardForeground: tokens.color.text,
  secondary: tokens.color.bg,
  secondaryForeground: tokens.color.text,
  muted: tokens.color.bg,
  mutedForeground: tokens.color.muted,
  accent: "#e7e5e4",
  accentForeground: tokens.color.text,
  destructive: tokens.color.danger,
  border: "#e5e7eb",
  input: "#d1d5db",
  ring: tokens.color.primary,
};

/** @type {import('tailwindcss').Config} */
module.exports = {
  content: ["./src/app/**/*.{js,jsx,ts,tsx}", "./src/components/**/*.{js,jsx,ts,tsx}"],
  presets: [require("nativewind/preset")],
  theme: {
    extend: {
      colors: {
        primary: { DEFAULT: savdo.primary, foreground: savdo.primaryForeground },
        background: savdo.background,
        foreground: savdo.foreground,
        card: { DEFAULT: savdo.card, foreground: savdo.cardForeground },
        secondary: { DEFAULT: savdo.secondary, foreground: savdo.secondaryForeground },
        muted: { DEFAULT: savdo.muted, foreground: savdo.mutedForeground },
        accent: { DEFAULT: savdo.accent, foreground: savdo.accentForeground },
        destructive: savdo.destructive,
        border: savdo.border,
        input: savdo.input,
        ring: savdo.ring,
      },
      borderRadius: {
        md: `${tokens.radius.md}px`,
        lg: `${tokens.radius.lg}px`,
      },
    },
  },
  plugins: [],
};
