import { useTranslation } from "react-i18next";

import {
  type Availability,
  availabilityToneClass,
  availabilityTranslationKey,
} from "../lib/availability";

export function AvailabilityBadge({ value }: { value: Availability }) {
  const { t } = useTranslation();
  return (
    <span
      className={`inline-flex items-center rounded-sm px-2 py-0.5 text-xs font-medium ${availabilityToneClass(value)}`}
    >
      {t(availabilityTranslationKey(value))}
    </span>
  );
}
