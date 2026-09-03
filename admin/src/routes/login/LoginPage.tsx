import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Alert, Button, Card, Form, Input, Tag } from "antd";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { ApiAuthError, login } from "../../auth/api";

interface LoginFormValues {
  username: string;
  password: string;
}

/** Translation key for `auth.errors.*`, chosen from the API's `ErrorCode`
 * (ADR-013) — a raw API sentence is never shown (ADR-013, AGENTS.md). */
function errorKeyFor(error: unknown): "invalidCredentials" | "rateLimited" | "generic" {
  if (error instanceof ApiAuthError) {
    if (error.code === "UNAUTHENTICATED") {
      return "invalidCredentials";
    }
    if (error.code === "RATE_LIMITED") {
      return "rateLimited";
    }
  }
  return "generic";
}

export function LoginPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [submitting, setSubmitting] = useState(false);
  const [errorKey, setErrorKey] = useState<string | null>(null);
  const [retryAfterSeconds, setRetryAfterSeconds] = useState<number | null>(null);

  async function handleFinish(values: LoginFormValues) {
    setSubmitting(true);
    setErrorKey(null);
    setRetryAfterSeconds(null);
    try {
      await login(values);
      await queryClient.invalidateQueries({ queryKey: ["auth", "me"] });
      await navigate({ to: "/" });
    } catch (error) {
      setErrorKey(errorKeyFor(error));
      setRetryAfterSeconds(
        error instanceof ApiAuthError ? (error.retryAfterSeconds ?? null) : null,
      );
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div
      style={{
        minHeight: "100vh",
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        padding: 24,
      }}
    >
      <Card title={t("auth.login.title")} style={{ width: 360 }}>
        {errorKey && (
          <Alert
            style={{ marginBottom: 16 }}
            type="error"
            showIcon
            message={t(`auth.errors.${errorKey}`)}
            description={
              retryAfterSeconds != null ? <Tag>{`~${retryAfterSeconds}s`}</Tag> : undefined
            }
          />
        )}
        <Form<LoginFormValues> layout="vertical" onFinish={handleFinish} disabled={submitting}>
          <Form.Item name="username" label={t("auth.login.username")} rules={[{ required: true }]}>
            <Input autoComplete="username" />
          </Form.Item>
          <Form.Item
            name="password"
            label={t("auth.login.password")}
            rules={[{ required: true, min: 8 }]}
          >
            <Input.Password autoComplete="current-password" />
          </Form.Item>
          <Form.Item>
            <Button type="primary" htmlType="submit" loading={submitting} block>
              {t("auth.login.submit")}
            </Button>
          </Form.Item>
        </Form>
      </Card>
    </div>
  );
}
