import { useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { Alert, Button, Card, Divider, Form, Input } from "antd";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { ApiAuthError, authenticateTelegram, login } from "../../auth/api";
import { TelegramLoginButton, type TelegramWidgetUser } from "../../auth/TelegramLoginButton";

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

/** Full i18n key for a `POST /auth/telegram` failure — `401
 * UNAUTHENTICATED` here specifically means "HMAC ok, but this Telegram
 * account isn't linked to any user" (or a bad HMAC; the API deliberately
 * returns the same code for both, docs/05-API.md § Auth), a different
 * message from the username/password form's `invalidCredentials`. */
function telegramErrorKeyFor(error: unknown): string {
  if (error instanceof ApiAuthError) {
    if (error.code === "UNAUTHENTICATED") {
      return "auth.telegram.errors.notLinked";
    }
    if (error.code === "RATE_LIMITED") {
      return "auth.errors.rateLimited";
    }
  }
  return "auth.errors.generic";
}

export function LoginPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [submitting, setSubmitting] = useState(false);
  const [errorKey, setErrorKey] = useState<string | null>(null);
  const [retryAfterSeconds, setRetryAfterSeconds] = useState<number | null>(null);
  // Separate from `errorKey`/`retryAfterSeconds`: the Telegram widget
  // callback fires outside the AntD form submit above and needs its own
  // message set (`telegramErrorKeyFor`, distinct wording for the same
  // `UNAUTHENTICATED` code) rather than sharing `auth.errors.*`.
  const [telegramErrorKey, setTelegramErrorKey] = useState<string | null>(null);

  const botUsername = import.meta.env.VITE_BOT_USERNAME;

  async function afterAuthenticated() {
    await queryClient.invalidateQueries({ queryKey: ["auth", "me"] });
    await navigate({ to: "/" });
  }

  async function handleFinish(values: LoginFormValues) {
    setSubmitting(true);
    setErrorKey(null);
    setRetryAfterSeconds(null);
    setTelegramErrorKey(null);
    try {
      await login(values);
      await afterAuthenticated();
    } catch (error) {
      setErrorKey(errorKeyFor(error));
      setRetryAfterSeconds(
        error instanceof ApiAuthError ? (error.retryAfterSeconds ?? null) : null,
      );
    } finally {
      setSubmitting(false);
    }
  }

  async function handleTelegramAuth(user: TelegramWidgetUser) {
    setSubmitting(true);
    setErrorKey(null);
    setRetryAfterSeconds(null);
    setTelegramErrorKey(null);
    try {
      await authenticateTelegram({
        id: String(user.id),
        firstName: user.first_name,
        lastName: user.last_name,
        username: user.username,
        photoUrl: user.photo_url,
        authDate: user.auth_date,
        hash: user.hash,
      });
      await afterAuthenticated();
    } catch (error) {
      setTelegramErrorKey(telegramErrorKeyFor(error));
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
              retryAfterSeconds != null
                ? t("auth.errors.retryAfter", { seconds: retryAfterSeconds })
                : undefined
            }
          />
        )}
        {telegramErrorKey && (
          <Alert
            style={{ marginBottom: 16 }}
            type="error"
            showIcon
            message={t(telegramErrorKey)}
            description={
              retryAfterSeconds != null
                ? t("auth.errors.retryAfter", { seconds: retryAfterSeconds })
                : undefined
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
        <div style={{ textAlign: "right", marginBottom: botUsername ? 16 : 0 }}>
          <Link to="/forgot-password">{t("auth.login.forgotPassword")}</Link>
        </div>
        {botUsername && (
          <>
            <Divider plain>{t("auth.login.orDivider")}</Divider>
            <div style={{ display: "flex", justifyContent: "center" }}>
              <TelegramLoginButton botUsername={botUsername} onAuth={handleTelegramAuth} />
            </div>
          </>
        )}
      </Card>
    </div>
  );
}
