import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import { ContactsBlock } from "../../components/ContactsBlock";
import { HoursTable } from "../../components/HoursTable";
import { ClockIcon, MapPinIcon } from "../../components/icons";
import { absoluteUrl, asRouteMatchHead, buildSeoHead, chooseOgImage } from "../../lib/seo";
import { splitParagraphs } from "../../lib/text";

/** About/contacts page (deliverable 1): `about.title`/`body`, the hours
 * table, contacts and social — all D-99 content blocks already fetched by
 * the `$locale` layout, so this route needs no loader of its own. Any
 * block that was never set in the admin (D-104 — a whole key can be
 * absent, not just one locale) is simply skipped, the same conditional
 * pattern the home page uses. */
export const Route = createFileRoute("/$locale/about")({
  head: ({ match }) => {
    const { shop, locale, siteUrl } = match.context;
    const about = shop.blocks.about;
    const title = about?.title != null && about.title !== "" ? about.title : shop.name;
    const description =
      about?.body != null && about.body !== ""
        ? about.body.slice(0, 200)
        : (shop.blocks.seo?.description ?? shop.name);
    const image = chooseOgImage({
      siteUrl,
      heroImage: shop.blocks.hero?.image,
      fallbackUrl: absoluteUrl(siteUrl, "/og-default.png"),
    });
    return asRouteMatchHead(
      buildSeoHead({ siteUrl, locale, path: "/about", title, description, image }),
    );
  },
  component: AboutPage,
});

function AboutPage() {
  const { t } = useTranslation();
  const { shop } = Route.useRouteContext();
  const blocks = shop.blocks;
  const about = blocks.about;

  return (
    <main className="mx-auto flex max-w-3xl flex-col gap-8 px-4 py-8">
      <h1 className="font-bold text-2xl text-text">{about?.title ?? shop.name}</h1>

      {about?.body != null && about.body !== "" && (
        <div className="flex flex-col gap-3 text-text">
          {splitParagraphs(about.body).map((paragraph, index) => (
            // biome-ignore lint/suspicious/noArrayIndexKey: a static, never-reordered list of paragraphs split from one string (same pattern as the product page).
            <p key={index} className="whitespace-pre-line">
              {paragraph}
            </p>
          ))}
        </div>
      )}

      {blocks.hours != null && (
        <section>
          <h2 className="mb-4 flex items-center gap-2 font-semibold text-text text-xl">
            <ClockIcon className="h-5 w-5 text-primary" />
            {t("web.home.hoursTitle")}
          </h2>
          <HoursTable hours={blocks.hours} />
        </section>
      )}

      {blocks.contacts != null && (
        <section>
          <h2 className="mb-4 flex items-center gap-2 font-semibold text-text text-xl">
            <MapPinIcon className="h-5 w-5 text-primary" />
            {t("web.home.contactsTitle")}
          </h2>
          <ContactsBlock contacts={blocks.contacts} social={blocks.social} />
        </section>
      )}
    </main>
  );
}
