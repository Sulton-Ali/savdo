import { Link, useRouterState } from "@tanstack/react-router";
import type { MenuProps } from "antd";
import { Menu } from "antd";
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

interface NavLeaf {
  key: string;
  icon: ReactNode;
  label: ReactNode;
  /** A `me.permissions` capability string (ADR-010), or `null` for an item
   * every authenticated user sees regardless of role. */
  permission: string | null;
}

interface NavGroup {
  /** Unique menu key for the group header (`type: "group"`, D-122). */
  key: string;
  /** i18n key for the group's section label, e.g. `nav.groups.sales`. */
  titleKey: string;
  items: NavLeaf[];
}

/** Resolves the active menu key by longest-prefix match against `pathname`
 * (D-122): a nested route like `/products/new` highlights `/products`, and
 * `/sales/drafts` — itself a key — outranks the shorter `/sales` match for
 * `/sales/drafts/...`. `/` (dashboard) only matches the exact root path,
 * since every path starts with `/`. */
export function selectedNavKeys(pathname: string, keys: string[]): string[] {
  const match = keys
    .filter((key) =>
      key === "/" ? pathname === "/" : pathname === key || pathname.startsWith(`${key}/`),
    )
    .sort((a, b) => b.length - a.length)[0];
  return match ? [match] : [];
}

export interface NavMenuProps {
  /** `useAuth().can` — a permission-string predicate (ADR-010). The UI
   * hides what a role cannot do; the API stays the enforcement point. */
  can: (permission: string) => boolean;
  /** Called after a click on a nav item — closes the mobile drawer; a
   * no-op on the desktop sider. */
  onNavigate?: () => void;
}

/** The grouped, permission-filtered nav menu shared by the desktop sider
 * and the mobile drawer (D-122). Dashboard sits ungrouped on top; every
 * other item belongs to one section (`type: "group"`); a section with no
 * visible item (every child filtered out by permission) is dropped
 * entirely rather than shown as an empty header. */
export function NavMenu({ can, onNavigate }: NavMenuProps) {
  const { t } = useTranslation();
  const pathname = useRouterState({ select: (state) => state.location.pathname });

  const dashboardItem: NavLeaf = {
    key: "/",
    icon: <LayoutDashboard size={16} />,
    label: <Link to="/">{t("nav.dashboard")}</Link>,
    permission: null,
  };

  const groups: NavGroup[] = [
    {
      key: "sales",
      titleKey: "nav.groups.sales",
      items: [
        // Phase 4 T6b: quick sale — visible to every role (`docs/04-DATA-MODEL.md`
        // § 7: "Create sale, attach customer" is owner/manager/cashier).
        {
          key: "/quick-sale",
          icon: <Receipt size={16} />,
          label: <Link to="/quick-sale">{t("nav.quickSale")}</Link>,
          permission: null,
        },
        // Phase 4 T6c: sales list (cashier+, D-63).
        {
          key: "/sales",
          icon: <Receipt size={16} />,
          label: <Link to="/sales">{t("nav.sales")}</Link>,
          permission: null,
        },
        // Phase 5 T15: draft sales — shared across staff, visible to every
        // role (D-87, "Create sale" row of `docs/04-DATA-MODEL.md` § 7).
        {
          key: "/sales/drafts",
          icon: <ClipboardList size={16} />,
          label: <Link to="/sales/drafts">{t("nav.salesDrafts")}</Link>,
          permission: null,
        },
      ],
    },
    {
      key: "catalog",
      titleKey: "nav.groups.catalog",
      items: [
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
      ],
    },
    {
      key: "stock",
      titleKey: "nav.groups.stock",
      items: [
        // T6b: stock levels are visible to every role (D-40); movements and
        // low stock are manager+ (`stock.write`).
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
      ],
    },
    {
      key: "people",
      titleKey: "nav.groups.people",
      items: [
        // Phase 4 T6a: customers (cashier+ create/read, manager+ edit/delete
        // — the whole page is visible to every role, same as `/products`).
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
      ],
    },
    {
      key: "reports",
      titleKey: "nav.groups.reports",
      items: [
        // Phase 4 T6d: reports dashboard, visible to every role — the page
        // itself adapts to `reports.read` (manager+) vs `reports.own_day`
        // (cashier).
        {
          key: "/reports",
          icon: <BarChart3 size={16} />,
          label: <Link to="/reports">{t("nav.reports")}</Link>,
          permission: null,
        },
      ],
    },
    {
      key: "bot",
      titleKey: "nav.groups.bot",
      items: [
        // Phase 7 T6: bot conversations (owner/manager, `bot.read`,
        // `docs/04-DATA-MODEL.md` § 7).
        {
          key: "/bot/conversations",
          icon: <Bot size={16} />,
          label: <Link to="/bot/conversations">{t("nav.bot")}</Link>,
          permission: "bot.read",
        },
      ],
    },
    {
      key: "settings",
      titleKey: "nav.groups.settings",
      items: [
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
        // Phase 7 T7: Telegram account link — every role manages their own
        // link (contract: any authenticated role), unlike `/settings` above.
        {
          key: "/settings/telegram",
          icon: <Send size={16} />,
          label: <Link to="/settings/telegram">{t("nav.telegram")}</Link>,
          permission: null,
        },
      ],
    },
  ];

  const allKeys = [
    dashboardItem.key,
    ...groups.flatMap((group) => group.items.map((item) => item.key)),
  ];

  const items: MenuProps["items"] = [
    { key: dashboardItem.key, icon: dashboardItem.icon, label: dashboardItem.label },
    ...groups
      .map((group) => {
        const visible = group.items.filter(
          (item) => item.permission === null || can(item.permission),
        );
        if (visible.length === 0) {
          return null;
        }
        return {
          key: group.key,
          type: "group" as const,
          label: t(group.titleKey),
          children: visible.map(({ key, icon, label }) => ({ key, icon, label })),
        };
      })
      .filter((group): group is NonNullable<typeof group> => group !== null),
  ];

  return (
    <Menu
      mode="inline"
      selectedKeys={selectedNavKeys(pathname, allKeys)}
      items={items}
      onClick={onNavigate}
      style={{ borderInlineEnd: "none" }}
    />
  );
}
