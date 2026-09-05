/**
 * Colours below mirror `packages/ui-tokens/src/tokens.ts` (D-25) — kept in
 * sync by hand like `tokens.css` already is for the web apps, since
 * NativeWind reads this file directly (no CSS-variable theme here; Tailwind
 * 3.4 + NativeWind 4 don't need one and the mobile shell has no dark mode).
 * `border`/`input`/`secondary`/`accent`/`destructive` are neutral UI chrome,
 * not brand colours, so they aren't in `ui-tokens` — picked to read well
 * against `background`/`card`. These semantic names match the copied
 * react-native-reusables components in `src/components/ui`.
 */
const savdo = {
  primary: "#c2410c",
  primaryForeground: "#ffffff",
  background: "#ffffff",
  foreground: "#1f2937",
  card: "#ffffff",
  cardForeground: "#1f2937",
  secondary: "#f5f5f4",
  secondaryForeground: "#1f2937",
  muted: "#f5f5f4",
  mutedForeground: "#6b7280",
  accent: "#e7e5e4",
  accentForeground: "#1f2937",
  destructive: "#dc2626",
  border: "#e5e7eb",
  input: "#d1d5db",
  ring: "#c2410c",
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
        md: "8px",
        lg: "12px",
      },
    },
  },
  plugins: [],
};
