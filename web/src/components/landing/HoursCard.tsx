import type { components } from "@savdo/api-client";
import { HoursTable } from "../HoursTable";
import { ClockIcon } from "../icons";

type ContentHours = components["schemas"]["ContentHours"];

/**
 * The "opening hours" twin card (D-121): the white card shell + icon badge
 * + heading are new for Variant B, the table content is the existing
 * `HoursTable` unchanged. `/$locale/about` reuses this same card (phase-7.5
 * T3) so both pages match. `HoursTable` renders its own bordered white box;
 * the `[&>div]:` overrides below flatten that inner box so it does not nest
 * inside this card's own border/shadow as a visible double frame.
 */
export function HoursCard({ hours, title }: { hours: ContentHours; title: string }) {
  return (
    <section className="flex flex-col gap-4 rounded-landing-card bg-landing-surface p-5 shadow-landing-card sm:p-6">
      <h2 className="flex items-center gap-2.5 font-bold text-text text-xl">
        <span className="inline-flex h-10 w-10 items-center justify-center rounded-xl bg-[#d5ecea] text-landing-secondary">
          <ClockIcon className="h-5 w-5" />
        </span>
        {title}
      </h2>
      <div className="[&>div]:border-0 [&>div]:bg-transparent [&>div]:p-0">
        <HoursTable hours={hours} />
      </div>
    </section>
  );
}
