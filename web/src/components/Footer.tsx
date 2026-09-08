import type { components } from "@savdo/api-client";
import { useTranslation } from "react-i18next";

import { summarizeHours, weekdayShortTranslationKey } from "../lib/hours";
import type { Locale } from "../lib/locale";
import { isSafeHttpsUrl } from "../lib/url";
import { mapHref } from "./ContactsBlock";
import { ClockIcon, InstagramIcon, MapPinIcon, PhoneIcon, TelegramIcon } from "./icons";

type PublicShopBlocks = components["schemas"]["PublicShopBlocks"];

const footerLinkClass = "text-muted transition hover:text-text";
const socialPillClass =
  "inline-flex items-center gap-1.5 rounded-full bg-bg px-3 py-1.5 text-sm text-text transition hover:bg-primary hover:text-white";

/**
 * Site-wide footer: nav links, a one-line hours summary (`summarizeHours`)
 * and a one-line contacts/social row — deliberately no table or card here
 * (deliverable "compact footer"); the full `HoursTable`/`ContactsBlock`
 * cards stay on the home and about pages, which already show this same
 * data prominently in their own body.
 */
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
  const hours = blocks.hours;
  const contacts = blocks.contacts;
  const social = blocks.social;

  const hoursSummary =
    hours != null
      ? summarizeHours(
          hours.days,
          (day) => t(weekdayShortTranslationKey(day)),
          t("web.footer.hoursClosed"),
        )
      : null;
  const map = contacts != null ? mapHref(contacts) : null;
  const telegram =
    social?.telegram != null && isSafeHttpsUrl(social.telegram) ? social.telegram : null;
  const instagram =
    social?.instagram != null && isSafeHttpsUrl(social.instagram) ? social.instagram : null;

  return (
    <footer className="border-bg border-t bg-surface">
      <div className="mx-auto flex max-w-5xl flex-col gap-3 px-4 py-8 text-sm">
        <div className="flex flex-wrap gap-x-6 gap-y-2">
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

        {hoursSummary != null && hoursSummary !== "" && (
          <p className="flex items-center gap-2 text-muted">
            <ClockIcon className="h-4 w-4 shrink-0" />
            {hoursSummary}
          </p>
        )}

        {contacts != null && (
          <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
            <a
              href={`tel:${contacts.phone}`}
              aria-label={t("web.contacts.callAria", { phone: contacts.phone })}
              className="inline-flex items-center gap-1.5 text-text transition hover:text-primary"
            >
              <PhoneIcon />
              {contacts.phone}
            </a>
            {map != null ? (
              <a
                href={map}
                target="_blank"
                rel="noopener noreferrer"
                aria-label={t("web.contacts.mapAria")}
                className="inline-flex items-center gap-1.5 text-muted transition hover:text-primary"
              >
                <MapPinIcon className="h-4 w-4 shrink-0" />
                {contacts.address}
              </a>
            ) : (
              <span className="inline-flex items-center gap-1.5 text-muted">
                <MapPinIcon className="h-4 w-4 shrink-0" />
                {contacts.address}
              </span>
            )}
          </div>
        )}

        {(telegram != null || instagram != null) && (
          <div className="flex flex-wrap gap-2">
            {telegram != null && (
              <a
                href={telegram}
                target="_blank"
                rel="noopener noreferrer"
                aria-label={t("web.contacts.telegramAria")}
                className={socialPillClass}
              >
                <TelegramIcon />
                Telegram
              </a>
            )}
            {instagram != null && (
              <a
                href={instagram}
                target="_blank"
                rel="noopener noreferrer"
                aria-label={t("web.contacts.instagramAria")}
                className={socialPillClass}
              >
                <InstagramIcon />
                Instagram
              </a>
            )}
          </div>
        )}
      </div>
      <div className="border-bg border-t px-4 py-4 text-center text-muted text-sm">
        {t("web.footer.rights", { year: new Date().getUTCFullYear(), shop: shopName })}
      </div>
    </footer>
  );
}
