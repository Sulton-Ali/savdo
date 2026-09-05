import { Stack } from "expo-router";
import { useTranslation } from "react-i18next";

/**
 * Nested stack for the Sale tab (T4): `index` (the quick-sale screen)
 * keeps the outer Tabs header (shop name + settings, same as every other
 * tab — `headerShown: false` here avoids doubling it, mirrors
 * `products/_layout.tsx`, T2), while `list` (today's sales) and `[id]`
 * (a read-only sale detail, reachable from `list` or from a customer's
 * purchase history) get this Stack's own native header since they're
 * pushed on top rather than being the tab root. The outer
 * `(app)/_layout.tsx` points its "Sale" `Tabs.Screen` at this whole
 * directory (`name="sale"`, not `"sale/index"`) so this layout — not a
 * flat file — is what that tab renders.
 */
export default function SaleLayout() {
  const { t } = useTranslation();

  return (
    <Stack>
      <Stack.Screen name="index" options={{ headerShown: false }} />
      <Stack.Screen name="list" options={{ title: t("sales.listTitle") }} />
      <Stack.Screen name="[id]" options={{ title: t("sales.detail.title") }} />
    </Stack>
  );
}
