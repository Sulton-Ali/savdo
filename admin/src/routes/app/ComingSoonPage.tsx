import { Card } from "antd";
import { useTranslation } from "react-i18next";

/** Shared placeholder for the pages T8 fills in (Staff, Locations,
 * Settings) — nav-only in this phase (T7). */
export function ComingSoonPage({
  titleKey,
}: {
  titleKey: "nav.staff" | "nav.locations" | "nav.settings";
}) {
  const { t } = useTranslation();
  return <Card title={t(titleKey)}>{t("common.comingSoon")}</Card>;
}
