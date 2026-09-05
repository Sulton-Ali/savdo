import { Stack } from "expo-router";
import { useTranslation } from "react-i18next";

/**
 * Nested stack for the Stock tab (Phase 5 T5): `index` (levels + low stock,
 * every role) keeps the outer Tabs header (shop name + settings — same as
 * every other tab, `headerShown: false` here avoids doubling it); every
 * other screen here is a `stock.write`-only action pushed on top, so it
 * gets this Stack's own native header (title, automatic back button).
 * Mirrors `products/_layout.tsx`'s pattern exactly — see that file's
 * comment for why `headerShown` is keyed the way it is in `(app)/_layout.tsx`.
 */
export default function StockLayout() {
  const { t } = useTranslation();

  return (
    <Stack>
      <Stack.Screen name="index" options={{ headerShown: false }} />
      <Stack.Screen name="adjust" options={{ title: t("stock.actions.adjust") }} />
      <Stack.Screen name="purchases/index" options={{ title: t("purchases.title") }} />
      <Stack.Screen name="purchases/new" options={{ title: t("purchases.createTitle") }} />
      <Stack.Screen name="purchases/[id]" options={{ title: t("purchases.title") }} />
      <Stack.Screen name="variant/[id]" options={{ title: t("stock.movements.title") }} />
    </Stack>
  );
}
