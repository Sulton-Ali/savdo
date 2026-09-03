import { useQuery } from "@tanstack/react-query";
import { Alert, Card, Space, Spin } from "antd";
import { useTranslation } from "react-i18next";

import { useAuth } from "../../auth/AuthContext";
import { api } from "../../lib/api";

async function fetchHealthz() {
  const { data, error } = await api.GET("/healthz");
  if (error) {
    throw error;
  }
  return data;
}

/** Phase 1 dashboard: a welcome line plus the Phase 0 healthz check that
 * proved the admin app can reach the API through the generated client and
 * the dev proxy. Real widgets land in a later phase. */
export function DashboardPage() {
  const { t } = useTranslation();
  const { me } = useAuth();
  const { data, isPending, isError } = useQuery({
    queryKey: ["healthz"],
    queryFn: fetchHealthz,
  });

  return (
    <Space direction="vertical" style={{ width: "100%" }}>
      <Card title={t("nav.dashboard")}>{t("dashboard.welcome", { name: me.user.fullName })}</Card>
      <Card title="API">
        {isPending && <Spin />}
        {isError && <Alert type="error" message={t("common.error")} />}
        {data && <span>API: {data.status}</span>}
      </Card>
    </Space>
  );
}
