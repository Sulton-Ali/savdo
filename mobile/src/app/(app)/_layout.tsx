import { tokens } from "@savdo/ui-tokens";
import { Tabs } from "expo-router";
import { getFocusedRouteNameFromRoute } from "expo-router/react-navigation";
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
 *
 * The Products tab nests its own Stack (`products/_layout.tsx`, T2) for
 * `index` -> `[id]` -> `[id]/edit`; T4 (sale) and T5 (stock) will do the
 * same. Left alone, this outer `<Tabs>` header would render *in addition
 * to* that inner Stack's own header on every nested screen — the standard
 * React Navigation fix is to key the outer tab's `headerShown` off the
 * currently focused nested route name (`getFocusedRouteNameFromRoute`,
 * re-exported by expo-router 57 from its vendored `@react-navigation/core`
 * fork at `expo-router/react-navigation` — confirmed against that
 * package's own `build/react-navigation/{core,native}` sources, not
 * training data: expo-router 57 no longer depends on the external
 * `@react-navigation/*` packages at all). Only the nested Stack's own
 * `index` screen hides its header (`products/_layout.tsx`) to avoid the
 * reverse doubling when this outer header is the one showing.
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
          name="products"
          options={({ route }) => ({
            title: t("nav.products"),
            tabBarIcon: ({ color, size }) => <Package color={color} size={size} />,
            // Only the nested Stack's initial "index" route reuses this
            // outer header (shop name + settings) — "[id]" and
            // "[id]/edit" show that Stack's own header instead, never
            // both.
            headerShown: (getFocusedRouteNameFromRoute(route) ?? "index") === "index",
          })}
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
