import { tokens } from "@savdo/ui-tokens";
import { Tabs } from "expo-router";
import { Home, Package, Settings, ShoppingCart, Users, Warehouse } from "lucide-react-native";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Pressable } from "react-native";

import { SettingsSheet } from "@/components/SettingsSheet";
import { useSession } from "@/lib/session";

/**
 * Bottom tabs (deliverable 6), role-filtered per `04-DATA-MODEL.md` § 7:
 * every role sees Home/Sale/Products/Customers, Stock is manager+owner only
 * (this phase leaves staff/settings on the web, D-15 permission matrix).
 * The header shows the shop name and a settings button (language + logout).
 */
export default function AppTabsLayout() {
  const { t } = useTranslation();
  const { role, shop } = useSession();
  const [settingsOpen, setSettingsOpen] = useState(false);
  const canSeeStock = role === "owner" || role === "manager";

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
        <Tabs.Protected guard={canSeeStock}>
          <Tabs.Screen
            name="stock/index"
            options={{
              title: t("mobile.shell.tabs.stock"),
              tabBarIcon: ({ color, size }) => <Warehouse color={color} size={size} />,
            }}
          />
        </Tabs.Protected>
      </Tabs>
      <SettingsSheet visible={settingsOpen} onClose={() => setSettingsOpen(false)} />
    </>
  );
}
