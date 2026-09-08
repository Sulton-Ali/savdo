import { Drawer } from "expo-router/drawer";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { AppDrawerContent } from "@/components/AppDrawerContent";
import { SettingsSheet } from "@/components/SettingsSheet";

/**
 * The authenticated app's shell (T12/D-90, replacing the earlier flat
 * `<Tabs>` this file used to render directly): a `Drawer`
 * (`expo-router/drawer`, bundled with expo-router 57 — D-94, no new
 * dependency) wrapping the bottom-tabs group (`(tabs)/_layout.tsx`) as its
 * only screen. `headerShown: false` here delegates the header to
 * `(tabs)/_layout.tsx`, which renders it with a menu button that opens
 * *this* drawer. The drawer's own content is fully custom
 * (`AppDrawerContent`, replacing `DrawerItemList`'s default auto-generated
 * rows) since every row (Drafts, Customers, Purchases, Low stock,
 * Adjustment) is a push into a screen nested inside `(tabs)` rather than a
 * top-level `Drawer.Screen` of its own — Drafts and Customers were briefly
 * `Drawer.Screen` siblings here (T12) but that left their own Stacks with
 * no shell (no header, menu button or tab bar: their `index` hid its own
 * header assuming `(tabs)/_layout.tsx` would supply one, which stopped
 * being true once they moved out of `(tabs)`); T21 moved them back inside
 * `(tabs)` as hidden tabs (`(tabs)/customers/`, `(tabs)/drafts/`,
 * `href`-equivalent hiding in `(tabs)/_layout.tsx`) so they get the same
 * shell as every other page again. The header's settings button moved here
 * too — same `SettingsSheet` modal and behaviour as before, just opened
 * from a drawer row instead of a header icon.
 */
export default function AppDrawerLayout() {
  const { t } = useTranslation();
  const [settingsOpen, setSettingsOpen] = useState(false);

  return (
    <>
      <Drawer
        screenOptions={{ headerShown: false }}
        drawerContent={(props) => (
          <AppDrawerContent {...props} onOpenSettings={() => setSettingsOpen(true)} />
        )}
      >
        <Drawer.Screen name="(tabs)" options={{ title: t("app.name") }} />
      </Drawer>
      <SettingsSheet visible={settingsOpen} onClose={() => setSettingsOpen(false)} />
    </>
  );
}
