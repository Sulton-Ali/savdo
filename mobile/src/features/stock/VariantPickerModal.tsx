import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Modal, Pressable, View } from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";
import { Button } from "@/components/ui/button";
import { Text } from "@/components/ui/text";
import type { Variant } from "@/features/catalog/api";
import { VariantPicker } from "@/features/catalog/VariantPicker";

/**
 * A button that opens the shared `VariantPicker` (T2) in a full-screen
 * `Modal` — used by the adjustment form (`app/(app)/stock/adjust.tsx`) and
 * the new-purchase item editor (`app/(app)/stock/purchases/new.tsx`) to pick
 * one variant. A plain `Modal` (not a pushed Stack screen) because picking
 * happens inside an existing form the caller doesn't want to unmount.
 */
export function VariantPickerModal({
  locationId,
  excludeVariantIds,
  triggerLabel,
  disabled,
  onPick,
}: {
  locationId: string;
  excludeVariantIds?: string[];
  triggerLabel: string;
  disabled?: boolean;
  onPick: (variant: Variant, availableQty: string) => void;
}) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);

  return (
    <>
      <Button variant="outline" disabled={disabled} onPress={() => setOpen(true)}>
        <Text numberOfLines={1}>{triggerLabel}</Text>
      </Button>
      <Modal visible={open} animationType="slide" onRequestClose={() => setOpen(false)}>
        <SafeAreaView className="flex-1 bg-background" edges={["top", "bottom"]}>
          <View className="flex-row items-center justify-between px-4 pb-2">
            <Text variant="h4">{t("mobile.catalog.picker.searchPlaceholder")}</Text>
            <Pressable accessibilityRole="button" onPress={() => setOpen(false)}>
              <Text className="text-primary">{t("common.cancel")}</Text>
            </Pressable>
          </View>
          <View className="flex-1 px-4 pb-4">
            <VariantPicker
              locationId={locationId}
              excludeVariantIds={excludeVariantIds}
              onPick={(variant, qty) => {
                setOpen(false);
                onPick(variant, qty);
              }}
            />
          </View>
        </SafeAreaView>
      </Modal>
    </>
  );
}
