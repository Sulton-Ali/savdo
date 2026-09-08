import type { components } from "@savdo/api-client";
import { useTranslation } from "react-i18next";

import { formatHoursRange, sortHoursDays, weekdayTranslationKey } from "../lib/hours";

type ContentHours = components["schemas"]["ContentHours"];

export function HoursTable({ hours }: { hours: ContentHours }) {
  const { t } = useTranslation();
  return (
    <div>
      <table className="w-full text-sm">
        <tbody>
          {sortHoursDays(hours.days).map((day) => {
            const range = formatHoursRange(day);
            return (
              <tr key={day.day} className="border-bg border-t first:border-t-0">
                <th scope="row" className="py-1 pr-4 text-left font-normal text-muted">
                  {t(weekdayTranslationKey(day.day))}
                </th>
                <td className="py-1 text-text">{range ?? t("web.hours.closed")}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
      {hours.note != null && hours.note !== "" && (
        <p className="mt-2 text-muted text-sm">{hours.note}</p>
      )}
    </div>
  );
}
