import { Outlet } from "@tanstack/react-router";
import { Button, Drawer, Grid, Layout, Space, Tag, Typography } from "antd";
import { LogOut, Menu as MenuIcon } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { AuthProvider, useAuth } from "../../auth/AuthContext";
import { useMe } from "../../auth/useMe";
import { LanguageSwitcher } from "../../components/LanguageSwitcher";
import { NavMenu } from "../../components/NavMenu";
import { authenticatedRoute } from "./authenticatedRoute";

const { Header, Sider, Content } = Layout;
const { useBreakpoint } = Grid;

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

  // Ant Design's `lg` breakpoint (~992px, D-122). `screens.lg` is
  // `undefined` for the very first render, before the breakpoint has
  // actually been measured (resolved synchronously right after, in the
  // same commit) — default to desktop so a desktop load never flashes the
  // mobile shell.
  const screens = useBreakpoint();
  const isDesktop = screens.lg ?? true;
  const [drawerOpen, setDrawerOpen] = useState(false);

  return (
    <Layout style={{ height: "100vh", overflow: "hidden" }}>
      {isDesktop && (
        <Sider theme="light" width={220} style={{ display: "flex", flexDirection: "column" }}>
          <div style={{ padding: 16, fontWeight: 600, flex: "0 0 auto" }}>{me.shop.name}</div>
          {/* The nav scrolls independently of the page content when it
           * overflows the sider's height (D-122). */}
          <div style={{ flex: "1 1 auto", overflowY: "auto" }}>
            <NavMenu can={can} />
          </div>
        </Sider>
      )}
      <Layout>
        <Header
          style={{
            display: "flex",
            alignItems: "center",
            gap: 16,
            background: "#fff",
          }}
        >
          {!isDesktop && (
            <Button
              type="text"
              aria-label={t("nav.openMenu")}
              icon={<MenuIcon size={18} />}
              onClick={() => setDrawerOpen(true)}
            />
          )}
          <div
            style={{
              display: "flex",
              alignItems: "center",
              gap: 16,
              marginInlineStart: "auto",
              minWidth: 0,
            }}
          >
            <Space style={{ minWidth: 0 }}>
              <Typography.Text ellipsis style={{ maxWidth: 160 }}>
                {me.user.fullName}
              </Typography.Text>
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
          </div>
        </Header>
        {/* Only this area scrolls (D-122) — the header and sider stay in
         * place; `.ant-layout-content` already carries `flex: auto` and
         * `min-height: 0` from Ant's own layout styles, which is what lets
         * `overflow: auto` bound to the remaining viewport height instead
         * of growing with the page. */}
        <Content style={{ margin: 24, overflow: "auto" }}>
          <Outlet />
        </Content>
      </Layout>
      <Drawer
        title={me.shop.name}
        placement="left"
        open={drawerOpen}
        onClose={() => setDrawerOpen(false)}
        size={260}
        styles={{ body: { padding: 0 } }}
      >
        <NavMenu can={can} onNavigate={() => setDrawerOpen(false)} />
      </Drawer>
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
