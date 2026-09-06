import { Stack } from "expo-router";
import { useTranslation } from "react-i18next";

/**
 * Nested stack for the Quick sale tab (T4, restructured T12/D-90): `index`
 * (the quick-sale screen) keeps the outer Tabs header (shop name + menu
 * button, same as every other tab — `headerShown: false` here avoids
 * doubling it, mirrors `products/_layout.tsx`, T2), while `[id]` (a
 * read-only sale detail, reachable from the "Sales list" tab or from a
 * customer's purchase history) gets this Stack's own native header since
 * it's pushed on top rather than being the tab root. "Today's sales" is no
 * longer nested here — it's its own bottom tab now (`(tabs)/sales.tsx`,
 * D-91) — but `[id]` stays part of *this* tab's stack and is reached from
 * elsewhere by a plain cross-tab push (same mechanism
 * `customers/[id].tsx` already relies on to reach `/sale/{id}`), which
 * switches to this tab and pushes `[id]` on top of it. The outer
 * `(app)/(tabs)/_layout.tsx` points its "Quick sale" `Tabs.Screen` at this
 * whole directory (`name="sale"`, not `"sale/index"`) so this layout — not
 * a flat file — is what that tab renders.
 */
export default function SaleLayout() {
  const { t } = useTranslation();

  return (
    <Stack>
      <Stack.Screen name="index" options={{ headerShown: false }} />
      <Stack.Screen name="[id]" options={{ title: t("sales.detail.title") }} />
    </Stack>
  );
}
