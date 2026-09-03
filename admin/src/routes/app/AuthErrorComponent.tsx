import type { ErrorComponentProps } from "@tanstack/react-router";
import { Button, Result } from "antd";
import { useTranslation } from "react-i18next";

/**
 * Rendered by `authenticatedRoute`'s `errorComponent` for anything that
 * isn't an UNAUTHENTICATED `GET /auth/me` (a 500, a network failure, ...) —
 * those are outages, not logouts, so they must not silently redirect to
 * `/login` and discard a valid session.
 */
export function AuthErrorComponent({ reset }: ErrorComponentProps) {
  const { t } = useTranslation();
  return (
    <div
      style={{
        minHeight: "100vh",
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
      }}
    >
      <Result
        status="error"
        title={t("errors.serviceUnavailable")}
        extra={
          <Button type="primary" onClick={() => reset()}>
            {t("common.retry")}
          </Button>
        }
      />
    </div>
  );
}
