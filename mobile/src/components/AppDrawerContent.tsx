import { tokens } from "@savdo/ui-tokens";
import { router } from "expo-router";
import type { DrawerContentComponentProps } from "expo-router/drawer";
import { DrawerContentScrollView } from "expo-router/drawer";
import {
  AlertTriangle,
  ClipboardList,
  Settings,
  SlidersHorizontal,
  Truck,
  Users,
} from "lucide-react-native";
import type { ComponentType } from "react";
import { useTranslation } from "react-i18next";
import { Pressable, View } from "react-native";

import { Text } from "@/components/ui/text";
import { useSession } from "@/lib/session";

interface DrawerRowProps {
  label: string;
  Icon: ComponentType<{ color: string; size: number }>;
  onPress: () => void;
}

function DrawerRow({ label, Icon, onPress }: DrawerRowProps) {
  return (
    <Pressable
      accessibilityRole="button"
      className="min-h-12 flex-row items-center gap-3 rounded-md px-4 py-3 active:bg-accent"
      onPress={onPress}
    >
      <Icon color={tokens.color.text} size={20} />
      <Text>{label}</Text>
    </Pressable>
  );
}

/**
 * Custom drawer content (T12/D-90): every row here is a plain
 * `router.push` (or, for "Settings", opening the existing modal sheet) —
 * none of these destinations need to be declared as `<Drawer.Screen>`
 * children of `(app)/_layout.tsx`'s `Drawer` to be reachable this way,
 * since expo-router resolves any app path from anywhere regardless of
 * which navigator currently owns the screen (the same mechanism
 * `customers/[id].tsx` already relies on to cross from the Customers stack
 * into `/sale/{id}`, nested in a different tab's own stack). "Purchases",
 * "Low stock" and "Adjustment" stay nested inside the Stock tab's own
 * `_layout.tsx` Stack (T12 deliverable 2) — pushing to them from here
 * switches to that tab and pushes the target on top of its stack, tab bar
 * still visible, back button returns to the Stock tab's levels page.
 * "Customers" and "Drafts" (T14/D-87..D-90) are two more nested Stacks
 * inside `(tabs)` (`(app)/(tabs)/customers/`, `(app)/(tabs)/drafts/`),
 * hidden from the tab bar itself (`(tabs)/_layout.tsx`) rather than
 * top-level `Drawer.Screen`s — pushing to them from here behaves the same
 * as "Purchases"/"Low stock"/"Adjustment": switches to that hidden tab,
 * tab bar and header still visible (they were briefly `Drawer.Screen`
 * siblings in T12, which left them with no shell at all; T21 fixed that by
 * moving them back inside `(tabs)`).
 */
export function AppDrawerContent(
  props: DrawerContentComponentProps & { onOpenSettings: () => void },
) {
  const { t } = useTranslation();
  const { can, shop } = useSession();
  const canWriteStock = can("stock.write");

  function go(path: Parameters<typeof router.push>[0]) {
    props.navigation.closeDrawer();
    router.push(path);
  }

  function openSettings() {
    props.navigation.closeDrawer();
    props.onOpenSettings();
  }

  return (
    <DrawerContentScrollView {...props} contentContainerStyle={{ paddingTop: 24 }}>
      <View className="mb-2 px-4 pb-3">
        <Text variant="h4" numberOfLines={1}>
          {shop?.name ?? t("app.name")}
        </Text>
      </View>

      <DrawerRow label={t("nav.drafts")} Icon={ClipboardList} onPress={() => go("/drafts")} />
      <DrawerRow label={t("nav.customers")} Icon={Users} onPress={() => go("/customers")} />

      {canWriteStock && (
        <>
          <DrawerRow
            label={t("purchases.title")}
            Icon={Truck}
            onPress={() => go("/stock/purchases")}
          />
          <DrawerRow
            label={t("stock.low.title")}
            Icon={AlertTriangle}
            onPress={() => go("/stock/low")}
          />
          <DrawerRow
            label={t("stock.actions.adjust")}
            Icon={SlidersHorizontal}
            onPress={() => go("/stock/adjust")}
          />
        </>
      )}

      <DrawerRow label={t("settings.title")} Icon={Settings} onPress={openSettings} />
    </DrawerContentScrollView>
  );
}
