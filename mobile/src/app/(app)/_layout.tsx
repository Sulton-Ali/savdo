import { Drawer } from "expo-router/drawer";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { AppDrawerContent } from "@/components/AppDrawerContent";
import { SettingsSheet } from "@/components/SettingsSheet";

/**
 * The authenticated app's shell (T12/D-90, replacing the earlier flat
 * `<Tabs>` this file used to render directly): a `Drawer`
 * (`expo-router/drawer`, bundled with expo-router 57 — D-94, no new
 * dependency) wrapping the bottom-tabs group (`(tabs)/_layout.tsx`) plus
 * the Customers stack (`customers/_layout.tsx`, unchanged from Phase 5 T4)
 * and the Drafts stack (`drafts/_layout.tsx`, T14) as its three screens.
 * `headerShown: false` here delegates every header to
 * whichever nested navigator is actually showing (`(tabs)/_layout.tsx`
 * already renders its own header with a menu button that opens *this*
 * drawer, plus each Stack's own native header on a pushed screen) — the
 * same "outer layout hides its own header, inner layout owns it" pattern
 * the old `(app)/_layout.tsx` already used for its nested Stacks. The
 * drawer's own content is fully custom (`AppDrawerContent`, replacing
 * `DrawerItemList`'s default auto-generated rows) since half its rows
 * (Purchases, Low stock, Adjustment) are cross-tab pushes rather than
 * top-level `Drawer.Screen`s of their own. The header's settings button
 * moved here too — same `SettingsSheet` modal and behaviour as before,
 * just opened from a drawer row instead of a header icon.
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
        <Drawer.Screen name="drafts" options={{ title: t("nav.drafts") }} />
        <Drawer.Screen name="customers" options={{ title: t("nav.customers") }} />
      </Drawer>
      <SettingsSheet visible={settingsOpen} onClose={() => setSettingsOpen(false)} />
    </>
  );
}
