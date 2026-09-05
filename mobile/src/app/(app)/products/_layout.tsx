import { Stack } from "expo-router";
import { useTranslation } from "react-i18next";

/**
 * Nested stack for the Products tab (deliverables 3-5): `index` (the list)
 * keeps the outer Tabs header (shop name + settings, same as every other
 * tab — `headerShown: false` here avoids doubling it), while `[id]` and
 * `[id]/edit` get this Stack's own native header (title, automatic back
 * button) since they're pushed on top rather than being tab roots. The
 * outer `(app)/_layout.tsx` points its "Products" `Tabs.Screen` at this
 * whole directory (`name="products"`, not `"products/index"`) so this
 * layout — not a flat file — is what that tab renders.
 */
export default function ProductsLayout() {
  const { t } = useTranslation();

  return (
    <Stack>
      <Stack.Screen name="index" options={{ headerShown: false }} />
      <Stack.Screen name="[id]" options={{ title: t("catalog.products.title") }} />
      <Stack.Screen name="[id]/edit" options={{ title: t("catalog.products.editTitle") }} />
    </Stack>
  );
}
