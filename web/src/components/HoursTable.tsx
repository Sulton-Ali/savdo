import type { components } from "@savdo/api-client";
import { useTranslation } from "react-i18next";

import { formatHoursRange, sortHoursDays, weekdayTranslationKey } from "../lib/hours";

type ContentHours = components["schemas"]["ContentHours"];

/** A tidy two-column card (day / hours), zebra-striped for readability
 * (deliverable 4 — was a bare, unstyled table). */
export function HoursTable({ hours }: { hours: ContentHours }) {
  const { t } = useTranslation();
  return (
    <div className="rounded-lg border border-bg bg-surface p-4">
      <table className="w-full text-sm">
        <tbody>
          {sortHoursDays(hours.days).map((day, index) => {
            const range = formatHoursRange(day);
            return (
              <tr key={day.day} className={index % 2 === 1 ? "bg-bg/60" : ""}>
                <th
                  scope="row"
                  className="rounded-l-md px-2 py-1.5 text-left font-normal text-muted"
                >
                  {t(weekdayTranslationKey(day.day))}
                </th>
                <td className="rounded-r-md px-2 py-1.5 text-right text-text">
                  {range ?? t("web.hours.closed")}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
      {hours.note != null && hours.note !== "" && (
        <p className="mt-3 text-muted text-sm">{hours.note}</p>
      )}
    </div>
  );
}
