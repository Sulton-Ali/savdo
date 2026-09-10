import dayjs from "dayjs";
import type { TFunction } from "i18next";
import { describe, expect, it } from "vitest";

import { buildDateRangePresets } from "../dateRangePresets";

const identityT = ((key: string) => key) as TFunction;

describe("buildDateRangePresets", () => {
  it("labels each preset via the filterBar.presets.* keys, in order", () => {
    const presets = buildDateRangePresets(identityT);
    expect(presets.map((preset) => preset.label)).toEqual([
      "filterBar.presets.today",
      "filterBar.presets.last7Days",
      "filterBar.presets.last30Days",
      "filterBar.presets.thisMonth",
    ]);
  });

  it("ends every range at the end of today", () => {
    const today = dayjs().format("YYYY-MM-DD");
    for (const preset of buildDateRangePresets(identityT)) {
      expect(preset.value[1].format("YYYY-MM-DD")).toBe(today);
      expect(preset.value[1].hour()).toBe(23);
    }
  });

  it("'today' starts at the beginning of today", () => {
    const [today] = buildDateRangePresets(identityT);
    const start = today?.value[0];
    expect(start?.format("YYYY-MM-DD")).toBe(dayjs().format("YYYY-MM-DD"));
    expect(start?.hour()).toBe(0);
  });

  it("'last7Days' starts 6 days before today (7 days inclusive)", () => {
    const [, last7Days] = buildDateRangePresets(identityT);
    expect(last7Days?.value[0].format("YYYY-MM-DD")).toBe(
      dayjs().subtract(6, "day").format("YYYY-MM-DD"),
    );
  });

  it("'last30Days' starts 29 days before today (30 days inclusive)", () => {
    const [, , last30Days] = buildDateRangePresets(identityT);
    expect(last30Days?.value[0].format("YYYY-MM-DD")).toBe(
      dayjs().subtract(29, "day").format("YYYY-MM-DD"),
    );
  });

  it("'thisMonth' starts on the 1st of the current month", () => {
    const [, , , thisMonth] = buildDateRangePresets(identityT);
    expect(thisMonth?.value[0].format("YYYY-MM-DD")).toBe(
      dayjs().startOf("month").format("YYYY-MM-DD"),
    );
  });
});
