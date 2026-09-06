import { Stack } from "expo-router";
import { useTranslation } from "react-i18next";

/**
 * Nested stack for the Drafts drawer item (T14/D-87..D-90), same shape as
 * `customers/_layout.tsx`: `index` (the list) keeps the outer Tabs/Drawer
 * header, while `[id]` (detail: view, Pay, Edit, Delete) gets this Stack's
 * own native header.
 */
export default function DraftsLayout() {
  const { t } = useTranslation();

  return (
    <Stack>
      <Stack.Screen name="index" options={{ headerShown: false }} />
      <Stack.Screen name="[id]" options={{ title: t("mobile.drafts.detail.title") }} />
    </Stack>
  );
}
