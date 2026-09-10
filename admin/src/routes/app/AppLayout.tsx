import { Outlet } from "@tanstack/react-router";
import { Button, Drawer, Grid, Layout, Popover, Space, Tag, Typography } from "antd";
import { LogOut, Menu as MenuIcon, User } from "lucide-react";
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
        // Sider only forwards `style` to the outer `<aside>`; the element
        // that actually wraps `children` is `.ant-layout-sider-children`,
        // reachable only through the `styles.body` semantic slot (verified
        // against the installed antd 6.6.2 source, `Sider.js`). Without
        // this, the flex column below is inert: `.ant-layout-sider-children`
        // stays a plain block box, the nav `div`'s `flex`/`overflowY` do
        // nothing, and it silently grows past the viewport with the last
        // items unreachable (D-122 regression).
        <Sider
          theme="light"
          width={220}
          styles={{ body: { display: "flex", flexDirection: "column", height: "100%" } }}
        >
          <div style={{ padding: 16, fontWeight: 600, flex: "0 0 auto" }}>{me.shop.name}</div>
          {/* The nav scrolls independently of the page content when it
           * overflows the sider's height (D-122). `minHeight: 0` is required
           * for a flex child to actually shrink below its content size and
           * hand overflow to `overflowY: auto` instead of stretching the
           * flex column past the sider's height. */}
          <div style={{ flex: "1 1 auto", minHeight: 0, overflowY: "auto" }}>
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
            <>
              <Button
                type="text"
                aria-label={t("nav.openMenu")}
                icon={<MenuIcon size={18} />}
                onClick={() => setDrawerOpen(true)}
              />
              {/* Below `lg` the full desktop cluster (name + role tag +
               * three-option language switcher + icon-and-text logout
               * button) overflows a phone-width header (D-122 fix-pass,
               * MAJOR 2: 467px of content in a 390px header). Only the
               * user's name stays inline, truncated; the role, language
               * and logout move behind a compact icon button so the
               * header's `scrollWidth` never exceeds its `clientWidth`. */}
              <Typography.Text ellipsis style={{ flex: "1 1 auto", minWidth: 0 }}>
                {me.user.fullName}
              </Typography.Text>
              <Popover
                trigger="click"
                placement="bottomRight"
                content={
                  <Space direction="vertical" size={12} style={{ minWidth: 200 }}>
                    <Tag>{t(`roles.${me.user.role}`)}</Tag>
                    <LanguageSwitcher />
                    <Button
                      type="text"
                      icon={<LogOut size={16} />}
                      style={{ justifyContent: "flex-start" }}
                      onClick={() => {
                        void logout();
                      }}
                    >
                      {t("auth.logout")}
                    </Button>
                  </Space>
                }
              >
                <Button type="text" aria-label={t("nav.accountMenu")} icon={<User size={18} />} />
              </Popover>
            </>
          )}
          {isDesktop && (
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
          )}
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
