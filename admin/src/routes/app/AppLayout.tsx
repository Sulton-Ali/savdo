import { Link, Outlet, useRouterState } from "@tanstack/react-router";
import { Button, Layout, Menu, Space, Tag, Typography } from "antd";
import {
  AlertTriangle,
  BarChart3,
  Bot,
  Boxes,
  ClipboardList,
  Contact,
  FolderTree,
  History,
  Image,
  LayoutDashboard,
  LogOut,
  MapPin,
  Package,
  Receipt,
  Send,
  Settings,
  ShoppingCart,
  Tags,
  Truck,
  Users,
} from "lucide-react";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";

import { AuthProvider, useAuth } from "../../auth/AuthContext";
import { useMe } from "../../auth/useMe";
import { LanguageSwitcher } from "../../components/LanguageSwitcher";
import { authenticatedRoute } from "./authenticatedRoute";

const { Header, Sider, Content } = Layout;

interface NavItem {
  key: string;
  icon: ReactNode;
  label: ReactNode;
  /** A `me.permissions` capability string (ADR-010), or `null` for an item
   * every authenticated user sees regardless of role. */
  permission: string | null;
}

function AppShell() {
  const { t } = useTranslation();
  const { me: routeMe, can, logout } = useAuth();
  // `routeMe` is the `beforeLoad` snapshot from when this route match was
  // last (re)loaded — it does not update on its own when `["auth", "me"]`
  // is invalidated (e.g. after a settings save). Read the live query for
  // display so the shop name/user in the shell stay current; fall back to
  // the route snapshot for the very first render, before this subscription
  // has a value of its own (it won't in practice, since `beforeLoad`
  // already primed the same cache entry, but this keeps the shell correct
  // even if that entry were ever evicted).
  const { data: liveMe } = useMe();
  const me = liveMe ?? routeMe;
  const pathname = useRouterState({ select: (state) => state.location.pathname });

  const navItems: NavItem[] = [
    {
      key: "/",
      icon: <LayoutDashboard size={16} />,
      label: <Link to="/">{t("nav.dashboard")}</Link>,
      permission: null,
    },
    // Phase 4 T6b: quick sale — visible to every role (`docs/04-DATA-MODEL.md`
    // § 7: "Create sale, attach customer" is owner/manager/cashier).
    {
      key: "/quick-sale",
      icon: <Receipt size={16} />,
      label: <Link to="/quick-sale">{t("nav.quickSale")}</Link>,
      permission: null,
    },
    // Phase 4 T6c: sales list (cashier+, D-63). The quick-sale create entry
    // (a separate task) belongs right next to this one.
    {
      key: "/sales",
      icon: <Receipt size={16} />,
      label: <Link to="/sales">{t("nav.sales")}</Link>,
      permission: null,
    },
    // Phase 5 T15: draft sales — shared across staff, visible to every role
    // (D-87, "Create sale" row of `docs/04-DATA-MODEL.md` § 7).
    {
      key: "/sales/drafts",
      icon: <ClipboardList size={16} />,
      label: <Link to="/sales/drafts">{t("nav.salesDrafts")}</Link>,
      permission: null,
    },
    {
      key: "/products",
      icon: <Package size={16} />,
      label: <Link to="/products">{t("nav.products")}</Link>,
      permission: null,
    },
    {
      key: "/categories",
      icon: <FolderTree size={16} />,
      label: <Link to="/categories">{t("nav.categories")}</Link>,
      permission: "catalog.write",
    },
    {
      key: "/settings/attributes",
      icon: <Tags size={16} />,
      label: <Link to="/settings/attributes">{t("nav.attributes")}</Link>,
      permission: "catalog.write",
    },
    // T6b: stock levels are visible to every role (D-40); movements and low
    // stock are manager+ (`stock.write`).
    {
      key: "/stock",
      icon: <Boxes size={16} />,
      label: <Link to="/stock">{t("nav.stockLevels")}</Link>,
      permission: null,
    },
    {
      key: "/stock/movements",
      icon: <History size={16} />,
      label: <Link to="/stock/movements">{t("nav.stockMovements")}</Link>,
      permission: "stock.write",
    },
    {
      key: "/stock/low",
      icon: <AlertTriangle size={16} />,
      label: <Link to="/stock/low">{t("nav.stockLow")}</Link>,
      permission: "stock.write",
    },
    {
      key: "/staff",
      icon: <Users size={16} />,
      label: <Link to="/staff">{t("nav.staff")}</Link>,
      permission: "staff.manage",
    },
    {
      key: "/locations",
      icon: <MapPin size={16} />,
      label: <Link to="/locations">{t("nav.locations")}</Link>,
      permission: "locations.manage",
    },
    // Phase 4 T6a: customers (cashier+ create/read, manager+ edit/delete —
    // the whole page is visible to every role, same as `/products`).
    {
      key: "/customers",
      icon: <Contact size={16} />,
      label: <Link to="/customers">{t("nav.customers")}</Link>,
      permission: null,
    },
    // Phase 3 T6a: suppliers (manager+).
    {
      key: "/suppliers",
      icon: <Truck size={16} />,
      label: <Link to="/suppliers">{t("nav.suppliers")}</Link>,
      permission: "suppliers.manage",
    },
    // Phase 3 T6a: purchases (manager+).
    {
      key: "/purchases",
      icon: <ShoppingCart size={16} />,
      label: <Link to="/purchases">{t("nav.purchases")}</Link>,
      permission: "stock.write",
    },
    // Phase 4 T6d: reports dashboard, visible to every role — the page
    // itself adapts to `reports.read` (manager+) vs `reports.own_day`
    // (cashier).
    {
      key: "/reports",
      icon: <BarChart3 size={16} />,
      label: <Link to="/reports">{t("nav.reports")}</Link>,
      permission: null,
    },
    {
      key: "/settings",
      icon: <Settings size={16} />,
      label: <Link to="/settings">{t("nav.settings")}</Link>,
      permission: "shop.settings",
    },
    // Phase 6 T4: landing content editor (manager+, D-99).
    {
      key: "/settings/landing",
      icon: <Image size={16} />,
      label: <Link to="/settings/landing">{t("content.nav")}</Link>,
      permission: "content.manage",
    },
    // Phase 7 T6: bot conversations (owner/manager, `bot.read`,
    // `docs/04-DATA-MODEL.md` § 7).
    {
      key: "/bot/conversations",
      icon: <Bot size={16} />,
      label: <Link to="/bot/conversations">{t("nav.bot")}</Link>,
      permission: "bot.read",
    },
    // Phase 7 T7: Telegram account link — every role manages their own
    // link (contract: any authenticated role), unlike `/settings` above.
    {
      key: "/settings/telegram",
      icon: <Send size={16} />,
      label: <Link to="/settings/telegram">{t("nav.telegram")}</Link>,
      permission: null,
    },
  ];

  return (
    <Layout style={{ minHeight: "100vh" }}>
      <Sider theme="light" width={220}>
        <div style={{ padding: 16, fontWeight: 600 }}>{me.shop.name}</div>
        <Menu
          mode="inline"
          selectedKeys={[pathname]}
          items={navItems
            .filter((item) => item.permission === null || can(item.permission))
            .map(({ key, icon, label }) => ({ key, icon, label }))}
        />
      </Sider>
      <Layout>
        <Header
          style={{
            display: "flex",
            alignItems: "center",
            justifyContent: "flex-end",
            gap: 16,
            background: "#fff",
          }}
        >
          <Space>
            <Typography.Text>{me.user.fullName}</Typography.Text>
            <Tag>{t(`roles.${me.user.role}`)}</Tag>
          </Space>
          <LanguageSwitcher />
          <Button
            type="text"
            icon={<LogOut size={16} />}
            onClick={() => {
              void logout();
            }}
          >
            {t("auth.logout")}
          </Button>
        </Header>
        <Content style={{ margin: 24 }}>
          <Outlet />
        </Content>
      </Layout>
    </Layout>
  );
}

/** Resolves `me` from the route guard's `beforeLoad` context and exposes it
 * via `AuthProvider` to everything the layout renders. */
export function AppLayout() {
  const { me } = authenticatedRoute.useRouteContext();
  return (
    <AuthProvider me={me}>
      <AppShell />
    </AuthProvider>
  );
}
