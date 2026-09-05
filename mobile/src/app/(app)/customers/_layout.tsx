import { Stack } from "expo-router";
import { useTranslation } from "react-i18next";

/**
 * Nested stack for the Customers tab (T4), same shape as
 * `sale/_layout.tsx`/`products/_layout.tsx` (T2): `index` (search + list)
 * keeps the outer Tabs header, while `[id]` (detail + purchase history)
 * and `new` (create) get this Stack's own native header. `new` is reached
 * both from this tab's own "Add customer" button and, with a `returnTo`
 * param, from the Sale screen's customer row (`sale/index.tsx`,
 * `customers/new.tsx`'s own doc comment has the full round-trip).
 */
export default function CustomersLayout() {
  const { t } = useTranslation();

  return (
    <Stack>
      <Stack.Screen name="index" options={{ headerShown: false }} />
      <Stack.Screen name="[id]" options={{ title: t("customers.detail.title") }} />
      <Stack.Screen name="new" options={{ title: t("customers.add") }} />
    </Stack>
  );
}
