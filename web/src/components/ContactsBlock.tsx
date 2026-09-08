import type { components } from "@savdo/api-client";
import { useTranslation } from "react-i18next";

import { isSafeHttpsUrl } from "../lib/url";
import { InstagramIcon, MapPinIcon, PhoneIcon, TelegramIcon } from "./icons";

type ContentContacts = components["schemas"]["ContentContacts"];
type ContentSocial = components["schemas"]["ContentSocial"];

/** D-102: the address links to Yandex Maps, either the admin-set `mapUrl`
 * or the plain default search-by-address link (no embedded map, keeps the
 * page fast). `null` when neither is a safe `https:` link (O-19 "https
 * only") — the caller renders the address as plain text instead. */
function mapHref(contacts: ContentContacts): string | null {
  const href =
    contacts.mapUrl ?? `https://yandex.com/maps/?text=${encodeURIComponent(contacts.address)}`;
  return isSafeHttpsUrl(href) ? href : null;
}

const socialPillClass =
  "inline-flex items-center gap-1.5 rounded-full bg-bg px-3 py-1.5 text-sm text-text transition hover:bg-primary hover:text-white";

export function ContactsBlock({
  contacts,
  social,
}: {
  contacts: ContentContacts;
  social?: ContentSocial;
}) {
  const { t } = useTranslation();
  const map = mapHref(contacts);
  const telegram =
    social?.telegram != null && isSafeHttpsUrl(social.telegram) ? social.telegram : null;
  const instagram =
    social?.instagram != null && isSafeHttpsUrl(social.instagram) ? social.instagram : null;

  return (
    <address className="not-italic flex flex-col items-start gap-3">
      <a
        href={`tel:${contacts.phone}`}
        aria-label={t("web.contacts.callAria", { phone: contacts.phone })}
        className="inline-flex items-center gap-2 rounded-full bg-primary px-4 py-2 font-semibold text-white transition hover:bg-primary-hover"
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
          className="inline-flex items-center gap-2 text-text transition hover:text-primary"
        >
          <MapPinIcon className="h-4 w-4 shrink-0 text-muted" />
          <span>{contacts.address}</span>
        </a>
      ) : (
        <span className="inline-flex items-center gap-2 text-text">
          <MapPinIcon className="h-4 w-4 shrink-0 text-muted" />
          {contacts.address}
        </span>
      )}
      {(telegram != null || instagram != null) && (
        <span className="flex flex-wrap gap-2">
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
        </span>
      )}
    </address>
  );
}
