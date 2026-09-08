import type { components } from "@savdo/api-client";
import { useTranslation } from "react-i18next";

import type { Locale } from "../lib/locale";
import { ContactsBlock } from "./ContactsBlock";
import { HoursTable } from "./HoursTable";
import { ClockIcon, MapPinIcon } from "./icons";

type PublicShopBlocks = components["schemas"]["PublicShopBlocks"];

const footerLinkClass = "text-muted transition hover:text-text";

/** Site-wide nav, hours/contacts/social summary (deliverable 4), reusing
 * the same blocks and components the home/about pages render — present on
 * every page since a visitor may land anywhere, not only on the home
 * page. */
export function Footer({
  shopName,
  locale,
  blocks,
}: {
  shopName: string;
  locale: Locale;
  blocks: PublicShopBlocks;
}) {
  const { t } = useTranslation();
  const hasSummary = blocks.hours != null || blocks.contacts != null;

  return (
    <footer className="border-bg border-t bg-surface">
      <div className="mx-auto flex max-w-5xl flex-wrap gap-x-6 gap-y-2 px-4 pt-8 text-sm">
        <a href={`/${locale}`} className={footerLinkClass}>
          {t("web.nav.home")}
        </a>
        <a href={`/${locale}#products`} className={footerLinkClass}>
          {t("web.nav.catalog")}
        </a>
        <a href={`/${locale}/about`} className={footerLinkClass}>
          {t("web.nav.about")}
        </a>
      </div>
      {hasSummary && (
        <div className="mx-auto grid max-w-5xl gap-8 px-4 py-8 sm:grid-cols-2">
          {blocks.hours != null && (
            <div>
              <h2 className="mb-3 flex items-center gap-1.5 font-semibold text-muted text-xs uppercase tracking-wide">
                <ClockIcon className="h-3.5 w-3.5" />
                {t("web.home.hoursTitle")}
              </h2>
              <HoursTable hours={blocks.hours} />
            </div>
          )}
          {blocks.contacts != null && (
            <div>
              <h2 className="mb-3 flex items-center gap-1.5 font-semibold text-muted text-xs uppercase tracking-wide">
                <MapPinIcon className="h-3.5 w-3.5" />
                {t("web.home.contactsTitle")}
              </h2>
              <ContactsBlock contacts={blocks.contacts} social={blocks.social} />
            </div>
          )}
        </div>
      )}
      <div className="border-bg border-t px-4 py-4 text-center text-muted text-sm">
        {t("web.footer.rights", { year: new Date().getUTCFullYear(), shop: shopName })}
      </div>
    </footer>
  );
}
