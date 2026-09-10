import dayjs, { type Dayjs } from "dayjs";
import type { TFunction } from "i18next";

/** The four date-range presets every list-page date filter offers (D-124):
 * today, last 7 days, last 30 days, this month — all inclusive of today. */
const PRESET_KEYS = ["today", "last7Days", "last30Days", "thisMonth"] as const;

function presetRange(key: (typeof PRESET_KEYS)[number]): [Dayjs, Dayjs] {
  const end = dayjs().endOf("day");
  switch (key) {
    case "today":
      return [dayjs().startOf("day"), end];
    case "last7Days":
      return [dayjs().subtract(6, "day").startOf("day"), end];
    case "last30Days":
      return [dayjs().subtract(29, "day").startOf("day"), end];
    case "thisMonth":
      return [dayjs().startOf("month"), end];
  }
}

/**
 * Builds a `DatePicker.RangePicker` `presets` array (today / last 7 days /
 * last 30 days / this month, D-124) — pass straight to a `RangePicker`'s
 * `presets` prop; clicking one calls the picker's `onChange` with the
 * resolved `[Dayjs, Dayjs]` range, same as picking dates manually. Ranges
 * are computed at call time, not memoised by this function, so a preset
 * clicked near midnight still resolves to the current day.
 */
export function buildDateRangePresets(t: TFunction): { label: string; value: [Dayjs, Dayjs] }[] {
  return PRESET_KEYS.map((key) => ({
    label: t(`filterBar.presets.${key}`),
    value: presetRange(key),
  }));
}
