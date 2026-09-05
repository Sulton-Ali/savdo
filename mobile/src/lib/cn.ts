import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";

/** `class-variance-authority` + Tailwind class merge helper used by the
 * copied `react-native-reusables` components in `components/ui` (D-25,
 * D-78) — verbatim from the upstream `lib/utils.ts`. */
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}
