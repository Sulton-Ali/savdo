import { describe, expect, it } from "vitest";

import {
  formatHoursRange,
  sortHoursDays,
  summarizeHours,
  weekdayShortTranslationKey,
  weekdayTranslationKey,
} from "./hours";

describe("weekdayTranslationKey", () => {
  it("maps every weekday to its own key", () => {
    expect(weekdayTranslationKey("mon")).toBe("web.hours.mon");
    expect(weekdayTranslationKey("sun")).toBe("web.hours.sun");
  });
});

describe("weekdayShortTranslationKey", () => {
  it("maps every weekday to its own short-form key", () => {
    expect(weekdayShortTranslationKey("mon")).toBe("web.weekdayShort.mon");
    expect(weekdayShortTranslationKey("sun")).toBe("web.weekdayShort.sun");
  });
});

describe("formatHoursRange", () => {
  it("formats an open day as open–close", () => {
    expect(formatHoursRange({ day: "mon", closed: false, open: "09:00", close: "19:00" })).toBe(
      "09:00–19:00",
    );
  });

  it("returns null for a closed day", () => {
    expect(formatHoursRange({ day: "sun", closed: true })).toBeNull();
  });

  it("returns null when open/close is missing even though closed is false", () => {
    expect(formatHoursRange({ day: "mon", closed: false })).toBeNull();
    expect(formatHoursRange({ day: "mon", closed: false, open: "09:00" })).toBeNull();
  });
});

describe("sortHoursDays", () => {
  it("sorts a shuffled list into fixed mon→sun order", () => {
    const shuffled = [
      { day: "wed", closed: false, open: "09:00", close: "19:00" },
      { day: "sun", closed: true },
      { day: "mon", closed: false, open: "09:00", close: "19:00" },
      { day: "sat", closed: false, open: "10:00", close: "18:00" },
      { day: "fri", closed: false, open: "09:00", close: "19:00" },
      { day: "tue", closed: false, open: "09:00", close: "19:00" },
      { day: "thu", closed: false, open: "09:00", close: "19:00" },
    ] as const;

    expect(sortHoursDays(shuffled).map((d) => d.day)).toEqual([
      "mon",
      "tue",
      "wed",
      "thu",
      "fri",
      "sat",
      "sun",
    ]);
  });

  it("tolerates a short list (missing days simply don't appear)", () => {
    const partial = [
      { day: "sun", closed: true },
      { day: "mon", closed: false, open: "09:00", close: "19:00" },
    ] as const;

    expect(sortHoursDays(partial).map((d) => d.day)).toEqual(["mon", "sun"]);
  });
});

const SHORT: Record<string, string> = {
  mon: "Du",
  tue: "Se",
  wed: "Chor",
  thu: "Pay",
  fri: "Ju",
  sat: "Sh",
  sun: "Ya",
};
const shortDayLabel = (day: string) => SHORT[day] ?? day;
const CLOSED = "dam olish";

describe("summarizeHours", () => {
  it("collapses mon-fri into one range, keeps sat and sun as their own segments", () => {
    const days = [
      { day: "mon", closed: false, open: "09:00", close: "19:00" },
      { day: "tue", closed: false, open: "09:00", close: "19:00" },
      { day: "wed", closed: false, open: "09:00", close: "19:00" },
      { day: "thu", closed: false, open: "09:00", close: "19:00" },
      { day: "fri", closed: false, open: "09:00", close: "19:00" },
      { day: "sat", closed: false, open: "10:00", close: "18:00" },
      { day: "sun", closed: true },
    ] as const;

    expect(summarizeHours(days, shortDayLabel, CLOSED)).toBe(
      "Du–Ju 09:00–19:00 · Sh 10:00–18:00 · Ya dam olish",
    );
  });

  it("keeps every day as its own segment when no two neighbours match", () => {
    const days = [
      { day: "mon", closed: false, open: "09:00", close: "13:00" },
      { day: "tue", closed: false, open: "10:00", close: "14:00" },
      { day: "wed", closed: true },
      { day: "thu", closed: false, open: "09:00", close: "18:00" },
      { day: "fri", closed: false, open: "09:00", close: "18:00" },
      { day: "sat", closed: true },
      { day: "sun", closed: true },
    ] as const;

    // thu+fri share a range and merge; wed/sat/sun are each closed but not
    // adjacent to another closed day in every case (sat+sun do merge).
    expect(summarizeHours(days, shortDayLabel, CLOSED)).toBe(
      "Du 09:00–13:00 · Se 10:00–14:00 · Chor dam olish · Pay–Ju 09:00–18:00 · Sh–Ya dam olish",
    );
  });

  it("collapses all seven days into one segment when every day is identical", () => {
    const days = [
      { day: "mon", closed: false, open: "09:00", close: "18:00" },
      { day: "tue", closed: false, open: "09:00", close: "18:00" },
      { day: "wed", closed: false, open: "09:00", close: "18:00" },
      { day: "thu", closed: false, open: "09:00", close: "18:00" },
      { day: "fri", closed: false, open: "09:00", close: "18:00" },
      { day: "sat", closed: false, open: "09:00", close: "18:00" },
      { day: "sun", closed: false, open: "09:00", close: "18:00" },
    ] as const;

    expect(summarizeHours(days, shortDayLabel, CLOSED)).toBe("Du–Ya 09:00–18:00");
  });

  it("returns an empty string for no days", () => {
    expect(summarizeHours([], shortDayLabel, CLOSED)).toBe("");
  });

  it("sorts input into mon→sun order regardless of the array's own order", () => {
    const shuffled = [
      { day: "sun", closed: true },
      { day: "mon", closed: false, open: "09:00", close: "19:00" },
    ] as const;

    expect(summarizeHours(shuffled, shortDayLabel, CLOSED)).toBe("Du 09:00–19:00 · Ya dam olish");
  });
});
