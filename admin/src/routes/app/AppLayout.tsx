import { Link, Outlet, useRouterState } from "@tanstack/react-router";
import { Button, Layout, Menu, Space, Tag, Typography } from "antd";
import { LayoutDashboard, LogOut, MapPin, Settings, Users } from "lucide-react";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";

import { AuthProvider, useAuth } from "../../auth/AuthContext";
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
  const { me, can, logout } = useAuth();
  const pathname = useRouterState({ select: (state) => state.location.pathname });

  const navItems: NavItem[] = [
    {
      key: "/",
      icon: <LayoutDashboard size={16} />,
      label: <Link to="/">{t("nav.dashboard")}</Link>,
      permission: null,
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
    {
      key: "/settings",
      icon: <Settings size={16} />,
      label: <Link to="/settings">{t("nav.settings")}</Link>,
      permission: "shop.settings",
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
