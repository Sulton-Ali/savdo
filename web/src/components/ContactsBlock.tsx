import type { components } from "@savdo/api-client";
import { useTranslation } from "react-i18next";

type ContentContacts = components["schemas"]["ContentContacts"];
type ContentSocial = components["schemas"]["ContentSocial"];

/** D-102: the address links to Yandex Maps, either the admin-set `mapUrl`
 * or the plain default search-by-address link (no embedded map, keeps the
 * page fast). */
function mapHref(contacts: ContentContacts): string {
  return contacts.mapUrl ?? `https://yandex.com/maps/?text=${encodeURIComponent(contacts.address)}`;
}

export function ContactsBlock({
  contacts,
  social,
}: {
  contacts: ContentContacts;
  social?: ContentSocial;
}) {
  const { t } = useTranslation();
  return (
    <address className="not-italic flex flex-col gap-2 text-text">
      <a
        href={`tel:${contacts.phone}`}
        aria-label={t("web.contacts.callAria", { phone: contacts.phone })}
      >
        {contacts.phone}
      </a>
      <a
        href={mapHref(contacts)}
        target="_blank"
        rel="noreferrer"
        aria-label={t("web.contacts.mapAria")}
      >
        {contacts.address}
      </a>
      {(social?.telegram != null || social?.instagram != null) && (
        <span className="flex gap-4">
          {social.telegram != null && (
            <a
              href={social.telegram}
              target="_blank"
              rel="noreferrer"
              aria-label={t("web.contacts.telegramAria")}
            >
              Telegram
            </a>
          )}
          {social.instagram != null && (
            <a
              href={social.instagram}
              target="_blank"
              rel="noreferrer"
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
