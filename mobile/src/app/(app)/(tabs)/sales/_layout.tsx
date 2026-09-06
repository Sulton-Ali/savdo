import { Stack } from "expo-router";
import { useTranslation } from "react-i18next";

/**
 * Nested stack for the Sales list tab (T12 follow-up): `index` (the list)
 * keeps the outer Tabs header (shop name + menu button — same as every
 * other tab, `headerShown: false` here avoids doubling it, mirrors
 * `sale/_layout.tsx`/`products/_layout.tsx`), while `[id]` (the same
 * read-only sale detail `/sale/{id}` renders, via the shared
 * `features/sales/SaleDetail.tsx`) gets this Stack's own native header
 * since it's pushed on top rather than being the tab root — giving this
 * tab a normal back button and Android back behaviour instead of the
 * cross-tab-push-with-no-local-history a plain `router.push("/sale/{id}")`
 * from this list would have produced. The outer `(app)/(tabs)/_layout.tsx`
 * points its "Sales list" `Tabs.Screen` at this whole directory
 * (`name="sales"`, not `"sales/index"`) so this layout — not a flat file —
 * is what that tab renders.
 */
export default function SalesLayout() {
  const { t } = useTranslation();

  return (
    <Stack>
      <Stack.Screen name="index" options={{ headerShown: false }} />
      <Stack.Screen name="[id]" options={{ title: t("sales.detail.title") }} />
    </Stack>
  );
}
