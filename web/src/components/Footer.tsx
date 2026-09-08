import { useTranslation } from "react-i18next";

import { LanguageSwitcher } from "./LanguageSwitcher";

export function Footer({ shopName }: { shopName: string }) {
  const { t } = useTranslation();
  return (
    <footer className="border-bg border-t bg-surface">
      <div className="mx-auto flex max-w-5xl flex-col items-center gap-3 px-4 py-6 text-sm">
        <LanguageSwitcher />
        <p className="text-muted">
          {t("web.footer.rights", { year: new Date().getUTCFullYear(), shop: shopName })}
        </p>
      </div>
    </footer>
  );
}
