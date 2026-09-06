import { Stack } from "expo-router";
import { useTranslation } from "react-i18next";

/**
 * Nested stack for the Stock tab (Phase 5 T5, split T12/D-90): `index` is
 * now *only* the levels page (location picker + browse/search) and keeps
 * the outer Tabs header (shop name + menu button — same as every other
 * tab, `headerShown: false` here avoids doubling it). Every other screen
 * here is a `stock.write`-only action reached from the drawer (Low stock,
 * Purchases, Adjustment — D-90) via a plain `router.push`, which switches
 * to this tab and pushes the target on top of its stack — the same
 * cross-tab-push mechanism `customers/[id].tsx` already relies on for
 * `/sale/{id}`. Keeping these nested here (rather than moving the files
 * out of the tabs group) preserves their exact URLs (`/stock/low`,
 * `/stock/purchases`, `/stock/purchases/new`, `/stock/adjust`) unchanged,
 * per this task's brief. Mirrors `products/_layout.tsx`'s pattern exactly
 * — see that file's comment for why `headerShown` is keyed the way it is
 * in `(app)/(tabs)/_layout.tsx`.
 */
export default function StockLayout() {
  const { t } = useTranslation();

  return (
    <Stack>
      <Stack.Screen name="index" options={{ headerShown: false }} />
      <Stack.Screen name="low" options={{ title: t("stock.low.title") }} />
      <Stack.Screen name="adjust" options={{ title: t("stock.actions.adjust") }} />
      <Stack.Screen name="purchases/index" options={{ title: t("purchases.title") }} />
      <Stack.Screen name="purchases/new" options={{ title: t("purchases.createTitle") }} />
      <Stack.Screen name="purchases/[id]" options={{ title: t("purchases.title") }} />
      <Stack.Screen name="variant/[id]" options={{ title: t("stock.movements.title") }} />
    </Stack>
  );
}
