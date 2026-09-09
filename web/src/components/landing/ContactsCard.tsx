import type { components } from "@savdo/api-client";

import { ContactsBlock } from "../ContactsBlock";
import { MapPinIcon } from "../icons";

type ContentContacts = components["schemas"]["ContentContacts"];
type ContentSocial = components["schemas"]["ContentSocial"];

/**
 * The "contacts" twin card (D-121), matching `HoursCard`'s shell. The
 * content is the existing `ContactsBlock` unchanged, so the phone/address/
 * social styling (and the `mapHref` https-only rule) stays exactly as
 * reviewed there. `/$locale/about` reuses this same card (phase-7.5 T3) so
 * both pages match.
 */
export function ContactsCard({
  contacts,
  social,
  title,
}: {
  contacts: ContentContacts;
  social?: ContentSocial;
  title: string;
}) {
  return (
    <section className="flex flex-col gap-4 rounded-landing-card bg-landing-surface p-5 shadow-landing-card sm:p-6">
      <h2 className="flex items-center gap-2.5 font-bold text-text text-xl">
        <span className="inline-flex h-10 w-10 items-center justify-center rounded-xl bg-[#fbe2d9] text-landing-accent-hover">
          <MapPinIcon className="h-5 w-5" />
        </span>
        {title}
      </h2>
      <ContactsBlock contacts={contacts} social={social} />
    </section>
  );
}
