import { Redirect, useLocalSearchParams } from "expo-router";
import { useTranslation } from "react-i18next";
import { View } from "react-native";

import { Text } from "@/components/ui/text";
import { useSession } from "@/lib/session";

/**
 * Placeholder — deliverable 5. Protected for manager+ (D-77: editing a
 * product's own fields is T3's scope, not this task's); a cashier who
 * somehow reaches this URL is bounced back to the read-only product
 * screen rather than shown a 403-style dead end. Pushed within
 * `products/_layout.tsx`'s Stack, so the header (title from i18n, back
 * button) is native — nothing to render here beyond the placeholder body.
 */
export default function ProductEditScreen() {
  const { t } = useTranslation();
  const { id } = useLocalSearchParams<{ id: string }>();
  const { role } = useSession();
  const canEdit = role === "owner" || role === "manager";

  if (!canEdit) {
    return <Redirect href={`/products/${id}`} />;
  }

  return (
    <View className="flex-1 items-center justify-center gap-2 bg-background p-6">
      <Text variant="muted">{t("common.comingSoon")}</Text>
    </View>
  );
}
