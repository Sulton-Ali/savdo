import { resources } from "@savdo/i18n";
import { useQuery } from "@tanstack/react-query";
import { Alert, Card, Spin } from "antd";

import { api } from "../lib/api";

async function fetchHealthz() {
  const { data, error } = await api.GET("/healthz");
  if (error) {
    throw error;
  }
  return data;
}

/** Phase 0 hello: an Ant Design card that proves the admin app can reach the
 * API through the generated client and the dev proxy (`vite.config.ts`). */
export function HomePage() {
  const { data, isPending, isError } = useQuery({
    queryKey: ["healthz"],
    queryFn: fetchHealthz,
  });

  return (
    <div style={{ padding: 24 }}>
      <Card title={resources.uz.app.name}>
        {isPending && <Spin />}
        {isError && <Alert type="error" message={resources.uz.common.error} />}
        {data && <span>API: {data.status}</span>}
      </Card>
    </div>
  );
}
