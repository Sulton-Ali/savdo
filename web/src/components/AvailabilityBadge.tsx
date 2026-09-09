import { useTranslation } from "react-i18next";

import {
  type Availability,
  availabilityToneClass,
  availabilityTranslationKey,
} from "../lib/availability";

export function AvailabilityBadge({
  value,
  className = "",
}: {
  value: Availability;
  /** Extra classes for the caller's own layout (e.g. `self-start` when the
   * badge sits in a flex column that would otherwise stretch it — the
   * `w-fit` below already stops the stretch, this only guards browsers
   * that would still center/align it oddly on the cross axis). */
  className?: string;
}) {
  const { t } = useTranslation();
  return (
    <span
      className={`inline-flex w-fit items-center rounded-sm px-2 py-0.5 text-xs font-medium ${availabilityToneClass(value)}${className ? ` ${className}` : ""}`}
    >
      {t(availabilityTranslationKey(value))}
    </span>
  );
}
