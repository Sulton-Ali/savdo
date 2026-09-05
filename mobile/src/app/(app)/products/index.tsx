import { useTranslation } from "react-i18next";
import { View } from "react-native";

import { Text } from "@/components/ui/text";

/** Placeholder — a later Phase 5 task replaces this with the products list. */
export default function ProductsScreen() {
  const { t } = useTranslation();
  return (
    <View className="flex-1 items-center justify-center gap-2 bg-background p-6">
      <Text variant="h3">{t("nav.products")}</Text>
      <Text variant="muted">{t("common.comingSoon")}</Text>
    </View>
  );
}
