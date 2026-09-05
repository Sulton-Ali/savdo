import { tokens } from "@savdo/ui-tokens";
import { Tabs } from "expo-router";
import { Home, Package, Settings, ShoppingCart, Users, Warehouse } from "lucide-react-native";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Pressable } from "react-native";

import { SettingsSheet } from "@/components/SettingsSheet";
import { useSession } from "@/lib/session";

/**
 * Bottom tabs: every role sees all five tabs — cashiers keep a read-only
 * Stock tab per the `04-DATA-MODEL.md` § 7 / D-40 permission matrix (D-81
 * amends the earlier manager+owner-only gate, which misquoted that matrix).
 * Write actions inside a tab (receive, adjust, transfer stock, …) are gated
 * per-action by `useSession().can(...)` against `me.permissions` (e.g.
 * `stock.write`), not by hiding the tab itself — later Phase 5 tasks wire
 * that into the real screens. The header shows the shop name and a settings
 * button (language + logout).
 */
export default function AppTabsLayout() {
  const { t } = useTranslation();
  const { shop } = useSession();
  const [settingsOpen, setSettingsOpen] = useState(false);

  return (
    <>
      <Tabs
        screenOptions={{
          headerTitle: shop?.name ?? t("app.name"),
          headerRight: () => (
            <Pressable
              accessibilityRole="button"
              accessibilityLabel={t("settings.title")}
              onPress={() => setSettingsOpen(true)}
              className="mr-4"
            >
              <Settings color={tokens.color.text} size={22} />
            </Pressable>
          ),
          tabBarActiveTintColor: tokens.color.primary,
        }}
      >
        <Tabs.Screen
          name="index"
          options={{
            title: t("nav.dashboard"),
            tabBarIcon: ({ color, size }) => <Home color={color} size={size} />,
          }}
        />
        <Tabs.Screen
          name="sale/index"
          options={{
            title: t("nav.quickSale"),
            tabBarIcon: ({ color, size }) => <ShoppingCart color={color} size={size} />,
          }}
        />
        <Tabs.Screen
          name="products/index"
          options={{
            title: t("nav.products"),
            tabBarIcon: ({ color, size }) => <Package color={color} size={size} />,
          }}
        />
        <Tabs.Screen
          name="customers/index"
          options={{
            title: t("nav.customers"),
            tabBarIcon: ({ color, size }) => <Users color={color} size={size} />,
          }}
        />
        <Tabs.Screen
          name="stock/index"
          options={{
            title: t("mobile.shell.tabs.stock"),
            tabBarIcon: ({ color, size }) => <Warehouse color={color} size={size} />,
          }}
        />
      </Tabs>
      <SettingsSheet visible={settingsOpen} onClose={() => setSettingsOpen(false)} />
    </>
  );
}
