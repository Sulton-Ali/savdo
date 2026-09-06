import { useRouter } from "expo-router";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { ActivityIndicator, Pressable, RefreshControl, ScrollView, View } from "react-native";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Text } from "@/components/ui/text";
import type { SalesByProductRow, SalesSummaryReport } from "@/features/reports/api";
import { useLowStock, useSalesByProduct, useSalesSummary } from "@/features/reports/hooks";
import { periodRange, type ReportPeriod } from "@/features/reports/period";
import { formatMoney } from "@/lib/money";
import { useSession } from "@/lib/session";

/** One label/value line inside a summary card. */
function Row({ label, value }: { label: string; value: string }) {
  return (
    <View className="flex-row items-center justify-between py-1">
      <Text variant="muted">{label}</Text>
      <Text>{value}</Text>
    </View>
  );
}

function ErrorRow({ onRetry }: { onRetry: () => void }) {
  const { t } = useTranslation();
  return (
    <View className="items-start gap-2 py-2">
      <Text variant="muted">{t("errors.generic")}</Text>
      <Pressable accessibilityRole="button" onPress={onRetry}>
        <Text className="text-primary">{t("common.retry")}</Text>
      </Pressable>
    </View>
  );
}

/**
 * Today/net summary card (deliverable 3): sales count, revenue, refunds,
 * net revenue always shown; `margin` only when the response actually
 * carries it (ADR-010 — never assumed from role alone, mirrors
 * `admin/src/routes/app/ReportsPage.tsx`'s `SummaryCard`). For a cashier
 * this is always today, own sales, own refunds netted in (D-71); the
 * period toggle above this card is hidden for that role instead of being
 * disabled here, so there is nothing role-specific left to branch on in
 * this component itself.
 */
function SummaryCardBody({
  query,
  currency,
}: {
  query: { data?: SalesSummaryReport; isPending: boolean; isError: boolean; refetch: () => void };
  currency: string;
}) {
  const { t } = useTranslation();

  if (query.isPending) {
    return <ActivityIndicator />;
  }
  if (query.isError || !query.data) {
    return <ErrorRow onRetry={query.refetch} />;
  }
  const data = query.data;
  return (
    <View>
      <Row label={t("reports.summary.fields.salesCount")} value={String(data.salesCount)} />
      <Row
        label={t("reports.summary.fields.revenue")}
        value={formatMoney(data.revenue, currency)}
      />
      <Row
        label={t("reports.summary.fields.refunds")}
        value={formatMoney(data.refunds, currency)}
      />
      <Row
        label={t("reports.summary.fields.netRevenue")}
        value={formatMoney(data.netRevenue, currency)}
      />
      {data.margin != null && (
        <Row
          label={t("reports.summary.fields.margin")}
          value={formatMoney(data.margin, currency)}
        />
      )}
    </View>
  );
}

function TopProductsCard({
  query,
  currency,
}: {
  query: {
    data?: { items: SalesByProductRow[] };
    isPending: boolean;
    isError: boolean;
    refetch: () => void;
  };
  currency: string;
}) {
  const { t } = useTranslation();

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("mobile.home.topProducts.title")}</CardTitle>
      </CardHeader>
      <CardContent>
        {query.isPending ? (
          <ActivityIndicator />
        ) : query.isError ? (
          <ErrorRow onRetry={query.refetch} />
        ) : (query.data?.items.length ?? 0) === 0 ? (
          <Text variant="muted">{t("mobile.home.topProducts.empty")}</Text>
        ) : (
          query.data?.items
            .slice(0, 3)
            .map((row) => (
              <Row
                key={row.productId}
                label={row.productName}
                value={formatMoney(row.revenue, currency)}
              />
            ))
        )}
      </CardContent>
    </Card>
  );
}

/**
 * The `(app)` tabs' "Dashboard" home screen (Phase 5 T6): a greeting, this
 * period's sales summary (D-71's cashier own-day/own-refund scoping is
 * entirely server-side — this screen just renders whatever
 * `GET /reports/sales/summary` returns), a low-stock shortcut for
 * `stock.write`, and, for `reports.read` (manager+), a period toggle and
 * top products. A cashier sees none of the manager-only pieces: the UI
 * hides them, the API is the actual enforcement point (ADR-010).
 */
export default function HomeScreen() {
  const { t } = useTranslation();
  const router = useRouter();
  const { me, can, shop } = useSession();
  const currency = shop?.currency ?? "UZS";
  const timeZone = shop?.timezone ?? Intl.DateTimeFormat().resolvedOptions().timeZone;

  const isManager = can("reports.read");
  const canSeeLowStock = can("stock.write");

  const [period, setPeriod] = useState<ReportPeriod>("today");
  const range = useMemo(() => periodRange(period, timeZone), [period, timeZone]);

  // A cashier's own-day view always calls with no filters at all — the
  // server ignores `from`/`to` for that role anyway (D-55), but sending
  // them regardless would make the query key vary with a period toggle
  // this role never sees, refetching for no reason.
  const summaryQuery = useSalesSummary(isManager ? range : {});
  const byProductQuery = useSalesByProduct(range, isManager);
  const lowStockQuery = useLowStock(canSeeLowStock);

  const [refreshing, setRefreshing] = useState(false);
  const handleRefresh = async () => {
    setRefreshing(true);
    try {
      await Promise.all([
        summaryQuery.refetch(),
        isManager ? byProductQuery.refetch() : Promise.resolve(),
        canSeeLowStock ? lowStockQuery.refetch() : Promise.resolve(),
      ]);
    } finally {
      setRefreshing(false);
    }
  };

  const lowStockCount = lowStockQuery.data?.items.length ?? 0;
  const lowStockHasMore = lowStockQuery.data?.nextCursor != null;

  return (
    <ScrollView
      className="flex-1 bg-background"
      contentContainerStyle={{ padding: 16, gap: 16 }}
      refreshControl={<RefreshControl refreshing={refreshing} onRefresh={handleRefresh} />}
    >
      <View className="gap-1">
        <Text variant="h3">{t("dashboard.welcome", { name: me?.user.fullName ?? "" })}</Text>
        {shop?.name && <Text variant="muted">{shop.name}</Text>}
      </View>

      {isManager && (
        <View className="flex-row gap-2" accessibilityLabel={t("reports.presetLabel")}>
          {(["today", "last7Days"] as const).map((option) => (
            <Pressable
              key={option}
              accessibilityRole="button"
              accessibilityState={{ selected: period === option }}
              className={`h-9 flex-1 items-center justify-center rounded-md border ${
                period === option ? "border-primary bg-primary" : "border-border bg-background"
              }`}
              onPress={() => setPeriod(option)}
            >
              <Text className={period === option ? "text-primary-foreground" : undefined}>
                {t(`reports.presets.${option}`)}
              </Text>
            </Pressable>
          ))}
        </View>
      )}

      <Card>
        <CardHeader>
          <CardTitle>{t(isManager ? "reports.summary.title" : "reports.myDay.title")}</CardTitle>
        </CardHeader>
        <CardContent>
          <SummaryCardBody query={summaryQuery} currency={currency} />
        </CardContent>
      </Card>

      {canSeeLowStock && (
        <Card>
          <CardHeader>
            <CardTitle>{t("reports.lowStock.title")}</CardTitle>
          </CardHeader>
          <CardContent>
            {lowStockQuery.isPending ? (
              <ActivityIndicator />
            ) : lowStockQuery.isError ? (
              <ErrorRow onRetry={() => lowStockQuery.refetch()} />
            ) : (
              <View className="flex-row items-center justify-between">
                <Text>
                  {t("reports.lowStock.count", {
                    value: `${lowStockCount}${lowStockHasMore ? "+" : ""}`,
                  })}
                </Text>
                <Pressable accessibilityRole="button" onPress={() => router.push("/stock")}>
                  <Text className="text-primary">{t("reports.lowStock.link")}</Text>
                </Pressable>
              </View>
            )}
          </CardContent>
        </Card>
      )}

      {isManager && <TopProductsCard query={byProductQuery} currency={currency} />}
    </ScrollView>
  );
}
