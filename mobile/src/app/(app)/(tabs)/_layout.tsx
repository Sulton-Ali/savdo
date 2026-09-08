import { tokens } from "@savdo/ui-tokens";
import { Tabs, useNavigation } from "expo-router";
import { getFocusedRouteNameFromRoute } from "expo-router/react-navigation";
import { Home, Menu, Package, Receipt, ShoppingCart, Warehouse } from "lucide-react-native";
import { useTranslation } from "react-i18next";
import { Pressable } from "react-native";

import { useSession } from "@/lib/session";

/**
 * The five most-used bottom tabs (T12/D-90, replacing the earlier six-tab
 * bar that also carried Customers): Home, Quick sale, Sales list, Products,
 * Stock — in that order. Purchases, Low stock, Adjustment and Settings
 * moved to the drawer this layout is nested inside (`(app)/_layout.tsx`'s
 * `Drawer`, `components/AppDrawerContent.tsx`); Customers and Drafts are
 * also reached from that drawer but stay nested here as hidden tabs (see
 * the doc comment further down) rather than leaving `(tabs)` entirely.
 * This `<Tabs>` no longer renders its own settings button — `headerLeft`
 * opens the parent drawer instead (menu button *and* an edge swipe, both
 * handled by `expo-router/drawer` itself). `navigation.dispatch({type:
 * "TOGGLE_DRAWER"})` is the drawer-router action type
 * (`expo-router/build/react-navigation/routers/DrawerRouter.js`) — it
 * bubbles up from this nested `Tabs` navigator to the parent `Drawer` the
 * same way any unhandled action does, so no `navigation.getParent()`
 * plumbing is needed; this is exactly what the vendored
 * `DrawerToggleButton` does internally, reimplemented here with a lucide
 * icon instead of that component's bundled PNG so it matches this header's
 * existing icon style (`Settings` before it, `Menu` now).
 *
 * "Sale", "Sales list", "Products" and "Stock" all nest their own Stack
 * (`_layout.tsx` in each) for a list -> detail/action push; this outer
 * `<Tabs>` header would otherwise render *in addition to* that inner
 * Stack's own header on every nested screen — `headerShown` is keyed off
 * the currently focused nested route name (`getFocusedRouteNameFromRoute`,
 * re-exported by expo-router 57 from its vendored `@react-navigation/core`
 * fork at `expo-router/react-navigation` — confirmed against that
 * package's own `build/react-navigation/{core,native}` sources, not
 * training data: expo-router 57 no longer depends on the external
 * `@react-navigation/*` packages at all) so only each nested Stack's own
 * `index` screen reuses this outer header. "Home" is the only flat,
 * single-screen tab with nothing nested under it, so it always shows this
 * outer header — no such gating needed for it.
 *
 * "Customers" and "Drafts" (T21, fixing a T12 regression) are two more
 * nested Stacks here, same shape and same `headerShown` gating as the four
 * above, but hidden from the tab bar itself since D-90 keeps them out of
 * the five visible tabs (they're reached from the drawer,
 * `components/AppDrawerContent.tsx`) — `getFocusedRouteNameFromRoute`
 * needs the per-tab `route` the options-resolver function receives to read
 * each hidden tab's own nested Stack state, which is why they can't use
 * the `href: null` shortcut `Tabs.Screen` otherwise supports for hiding a
 * tab: that shortcut is only applied by expo-router's `Tabs` wrapper when
 * `options` is a plain object, not a function (confirmed against the
 * installed package,
 * `node_modules/expo-router/build/layouts/TabsClient.js`: `typeof
 * screen.options !== 'function' && screen.options?.href !== undefined`) —
 * with a function-form `options` (needed here for the gating) `href` would
 * silently be ignored and the tab would stay visible. `tabBarItemStyle:
 * { display: "none" }` plus `tabBarButton: () => null` reproduce exactly
 * what that shortcut does internally, so the tab is hidden the same way
 * (not clickable, no bar space) while still being reachable by
 * `router.push`/`Link`, which don't go through `tabBarButton` at all.
 */
export default function AppTabsLayout() {
  const { t } = useTranslation();
  const { shop } = useSession();
  const navigation = useNavigation();

  function openDrawer() {
    navigation.dispatch({ type: "TOGGLE_DRAWER" });
  }

  return (
    <Tabs
      screenOptions={{
        headerTitle: shop?.name ?? t("app.name"),
        headerLeft: () => (
          <Pressable
            accessibilityRole="button"
            accessibilityLabel={t("mobile.shell.menu")}
            onPress={openDrawer}
            className="ml-4"
          >
            <Menu color={tokens.color.text} size={22} />
          </Pressable>
        ),
        tabBarActiveTintColor: tokens.color.primary,
      }}
    >
      <Tabs.Screen
        name="index"
        options={{
          // A mobile-only, shorter tab label — `nav.dashboard` ("Boshqaruv
          // paneli"/"Панель управления") is fine for the admin web sidebar
          // and this screen's own header, but truncates in the narrow tab
          // bar (review nit).
          title: t("mobile.shell.tabs.home"),
          tabBarIcon: ({ color, size }) => <Home color={color} size={size} />,
        }}
      />
      <Tabs.Screen
        name="sale"
        options={({ route }) => ({
          // `nav.quickSale` ("Быстрая продажа" in Russian) truncates in the
          // tab bar — same reason "index" below uses its own mobile-only
          // key instead of `nav.dashboard` (review nit).
          title: t("mobile.shell.tabs.quickSale"),
          tabBarIcon: ({ color, size }) => <ShoppingCart color={color} size={size} />,
          // Same rule as "products"/"stock" below: only the nested Stack's
          // initial "index" route reuses this outer header.
          headerShown: (getFocusedRouteNameFromRoute(route) ?? "index") === "index",
        })}
      />
      <Tabs.Screen
        name="sales"
        options={({ route }) => ({
          title: t("sales.listTitle"),
          tabBarIcon: ({ color, size }) => <Receipt color={color} size={size} />,
          headerShown: (getFocusedRouteNameFromRoute(route) ?? "index") === "index",
        })}
      />
      <Tabs.Screen
        name="products"
        options={({ route }) => ({
          title: t("nav.products"),
          tabBarIcon: ({ color, size }) => <Package color={color} size={size} />,
          headerShown: (getFocusedRouteNameFromRoute(route) ?? "index") === "index",
        })}
      />
      <Tabs.Screen
        name="stock"
        options={({ route }) => ({
          title: t("mobile.shell.tabs.stock"),
          tabBarIcon: ({ color, size }) => <Warehouse color={color} size={size} />,
          headerShown: (getFocusedRouteNameFromRoute(route) ?? "index") === "index",
        })}
      />
      <Tabs.Screen
        name="drafts"
        options={({ route }) => ({
          title: t("nav.drafts"),
          tabBarItemStyle: { display: "none" },
          tabBarButton: () => null,
          headerShown: (getFocusedRouteNameFromRoute(route) ?? "index") === "index",
        })}
      />
      <Tabs.Screen
        name="customers"
        options={({ route }) => ({
          title: t("nav.customers"),
          tabBarItemStyle: { display: "none" },
          tabBarButton: () => null,
          headerShown: (getFocusedRouteNameFromRoute(route) ?? "index") === "index",
        })}
      />
    </Tabs>
  );
}
