import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import { ContactsCard } from "../../components/landing/ContactsCard";
import { HoursCard } from "../../components/landing/HoursCard";
import { absoluteUrl, asRouteMatchHead, buildSeoHead, chooseOgImage } from "../../lib/seo";
import { splitParagraphs } from "../../lib/text";

/** About/contacts page (deliverable 1): `about.title`/`body`, the hours
 * table, contacts and social — all D-99 content blocks already fetched by
 * the `$locale` layout, so this route needs no loader of its own. Any
 * block that was never set in the admin (D-104 — a whole key can be
 * absent, not just one locale) is simply skipped, the same conditional
 * pattern the home page uses. phase-7.5 T3: the hours/contacts sections
 * reuse the homepage's own `HoursCard`/`ContactsCard` (white
 * `bg-landing-surface` cards) rather than the bare `HoursTable`/
 * `ContactsBlock`, so this page's cards match the home page's per D-121. */
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
    <main className="mx-auto flex max-w-3xl flex-col gap-6 px-4 py-8">
      <div className="flex flex-col gap-3 rounded-landing-card bg-landing-surface p-5 shadow-landing-card sm:p-6">
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
      </div>

      {(blocks.hours != null || blocks.contacts != null) && (
        <div className="grid gap-6 lg:grid-cols-2">
          {blocks.hours != null && (
            <HoursCard hours={blocks.hours} title={t("web.home.hoursTitle")} />
          )}

          {blocks.contacts != null && (
            <ContactsCard
              contacts={blocks.contacts}
              social={blocks.social}
              title={t("web.home.contactsTitle")}
            />
          )}
        </div>
      )}
    </main>
  );
}
