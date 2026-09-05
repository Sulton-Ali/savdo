import { Redirect, useLocalSearchParams, useRouter } from "expo-router";
import { ArrowLeft } from "lucide-react-native";
import { useTranslation } from "react-i18next";
import { Pressable, View } from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";

import { Text } from "@/components/ui/text";
import { useSession } from "@/lib/session";

/**
 * Placeholder — deliverable 5. Protected for manager+ (D-77: editing a
 * product's own fields is T3's scope, not this task's); a cashier who
 * somehow reaches this URL is bounced back to the read-only product
 * screen rather than shown a 403-style dead end.
 */
export default function ProductEditScreen() {
  const { t } = useTranslation();
  const router = useRouter();
  const { id } = useLocalSearchParams<{ id: string }>();
  const { role } = useSession();
  const canEdit = role === "owner" || role === "manager";

  if (!canEdit) {
    return <Redirect href={`/products/${id}`} />;
  }

  return (
    <SafeAreaView edges={["top"]} className="flex-1 bg-background">
      <View className="min-h-12 flex-row items-center gap-3 border-border border-b px-2 py-2">
        <Pressable
          accessibilityRole="button"
          accessibilityLabel={t("common.back")}
          className="h-12 w-12 items-center justify-center"
          onPress={() => router.back()}
        >
          <ArrowLeft size={22} />
        </Pressable>
        <Text variant="h4">{t("catalog.products.editTitle")}</Text>
      </View>
      <View className="flex-1 items-center justify-center gap-2 p-6">
        <Text variant="muted">{t("common.comingSoon")}</Text>
      </View>
    </SafeAreaView>
  );
}
