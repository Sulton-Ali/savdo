import type { components } from "@savdo/api-client";
import { useTranslation } from "react-i18next";

import { isSafeHttpsUrl } from "../lib/url";

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
    <address className="not-italic flex flex-col gap-2 text-text">
      <a
        href={`tel:${contacts.phone}`}
        aria-label={t("web.contacts.callAria", { phone: contacts.phone })}
      >
        {contacts.phone}
      </a>
      {map != null ? (
        <a
          href={map}
          target="_blank"
          rel="noopener noreferrer"
          aria-label={t("web.contacts.mapAria")}
        >
          {contacts.address}
        </a>
      ) : (
        <span>{contacts.address}</span>
      )}
      {(telegram != null || instagram != null) && (
        <span className="flex gap-4">
          {telegram != null && (
            <a
              href={telegram}
              target="_blank"
              rel="noopener noreferrer"
              aria-label={t("web.contacts.telegramAria")}
            >
              Telegram
            </a>
          )}
          {instagram != null && (
            <a
              href={instagram}
              target="_blank"
              rel="noopener noreferrer"
              aria-label={t("web.contacts.instagramAria")}
            >
              Instagram
            </a>
          )}
        </span>
      )}
    </address>
  );
}
