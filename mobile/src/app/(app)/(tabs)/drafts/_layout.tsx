import { Stack } from "expo-router";
import { useTranslation } from "react-i18next";

/**
 * Nested stack for the Drafts drawer item (T14/D-87..D-90), same shape as
 * `../customers/_layout.tsx`: `index` (the list) keeps the outer Tabs
 * header, while `[id]` (detail: view, Pay, Edit, Delete) gets this Stack's
 * own native header. This tab is hidden from the tab bar (D-90 keeps it
 * out of the five visible tabs, reached from the drawer instead —
 * `../_layout.tsx`'s own doc comment has why "hidden tab" rather than
 * "drawer screen", T21), but it's still a real `Tabs.Screen`, which is what
 * lets `index` keep reusing the outer header.
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
