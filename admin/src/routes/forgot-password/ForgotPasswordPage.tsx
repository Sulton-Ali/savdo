import { useNavigate } from "@tanstack/react-router";
import { Alert, App, Button, Card, Form, Input } from "antd";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import {
  ApiAuthError,
  requestPasswordResetOtp,
  resetPassword,
  verifyPasswordResetOtp,
} from "../../auth/api";

type Step = "username" | "code" | "password";

interface UsernameFormValues {
  username: string;
}

interface CodeFormValues {
  code: string;
}

interface PasswordFormValues {
  newPassword: string;
  confirmPassword: string;
}

/** Full `auth.errors.*`/`auth.forgotPassword.errors.*` i18n key for a
 * `requestPasswordResetOtp` failure (only `RATE_LIMITED` is realistic — the
 * endpoint answers `202` regardless of whether the username exists, ADR-013
 * / docs/05-API.md § Auth, no enumeration). */
function requestErrorKey(error: unknown): string {
  if (error instanceof ApiAuthError && error.code === "RATE_LIMITED") {
    return "auth.errors.rateLimited";
  }
  return "auth.errors.generic";
}

/** Full i18n key for a `verifyPasswordResetOtp` failure: `401
 * UNAUTHENTICATED` covers a wrong/expired/exhausted-attempts code, one
 * message for every reason (no enumeration). */
function verifyErrorKey(error: unknown): string {
  if (error instanceof ApiAuthError) {
    if (error.code === "UNAUTHENTICATED") {
      return "auth.forgotPassword.errors.codeInvalid";
    }
    if (error.code === "RATE_LIMITED") {
      return "auth.errors.rateLimited";
    }
  }
  return "auth.errors.generic";
}

/** Full i18n key for a `resetPassword` failure: `401 UNAUTHENTICATED` means
 * the `actionToken` expired or was already used — the flow restarts from
 * step 1 rather than staying on a dead-end step 3. */
function resetErrorKey(error: unknown): string {
  if (error instanceof ApiAuthError && error.code === "UNAUTHENTICATED") {
    return "auth.forgotPassword.errors.tokenExpired";
  }
  return "auth.errors.generic";
}

/**
 * Forgot-password wizard (Phase 7 T7, deliverable B): username → OTP code
 * (delivered by the bot to a linked Telegram account, ADR-005) →
 * `actionToken` → new password. `actionToken` is kept in component state
 * only (never `localStorage`/`sessionStorage`) — it's gone the moment this
 * page unmounts, same as every other in-memory credential in this app.
 */
export function ForgotPasswordPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { notification } = App.useApp();

  const [step, setStep] = useState<Step>("username");
  const [username, setUsername] = useState("");
  const [actionToken, setActionToken] = useState<string | null>(null);

  const [submitting, setSubmitting] = useState(false);
  const [errorMessageKey, setErrorMessageKey] = useState<string | null>(null);
  const [retryAfterSeconds, setRetryAfterSeconds] = useState<number | null>(null);

  function clearError() {
    setErrorMessageKey(null);
    setRetryAfterSeconds(null);
  }

  async function handleUsernameFinish(values: UsernameFormValues) {
    setSubmitting(true);
    clearError();
    try {
      await requestPasswordResetOtp(values.username);
      setUsername(values.username);
      setStep("code");
    } catch (error) {
      setErrorMessageKey(requestErrorKey(error));
      setRetryAfterSeconds(
        error instanceof ApiAuthError ? (error.retryAfterSeconds ?? null) : null,
      );
    } finally {
      setSubmitting(false);
    }
  }

  async function handleCodeFinish(values: CodeFormValues) {
    setSubmitting(true);
    clearError();
    try {
      const verified = await verifyPasswordResetOtp(username, values.code);
      setActionToken(verified.actionToken);
      setStep("password");
    } catch (error) {
      setErrorMessageKey(verifyErrorKey(error));
      setRetryAfterSeconds(
        error instanceof ApiAuthError ? (error.retryAfterSeconds ?? null) : null,
      );
    } finally {
      setSubmitting(false);
    }
  }

  async function handlePasswordFinish(values: PasswordFormValues) {
    if (!actionToken) {
      return;
    }
    setSubmitting(true);
    clearError();
    try {
      await resetPassword(actionToken, values.newPassword);
      notification.success({ message: t("auth.forgotPassword.success") });
      await navigate({ to: "/login" });
    } catch (error) {
      const key = resetErrorKey(error);
      setErrorMessageKey(key);
      if (key === "auth.forgotPassword.errors.tokenExpired") {
        // The action token is dead — there is nothing step 3 can retry;
        // send the user back to request a fresh code.
        setActionToken(null);
        setStep("username");
      }
    } finally {
      setSubmitting(false);
    }
  }

  const errorAlert = errorMessageKey && (
    <Alert
      style={{ marginBottom: 16 }}
      type="error"
      showIcon
      message={t(errorMessageKey)}
      description={
        retryAfterSeconds != null
          ? t("auth.errors.retryAfter", { seconds: retryAfterSeconds })
          : undefined
      }
    />
  );

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
      <Card title={t("auth.forgotPassword.title")} style={{ width: 360 }}>
        {errorAlert}

        {step === "username" && (
          <>
            <Alert
              style={{ marginBottom: 16 }}
              type="info"
              showIcon
              message={t("auth.forgotPassword.step1.description")}
            />
            <Form<UsernameFormValues>
              layout="vertical"
              onFinish={handleUsernameFinish}
              disabled={submitting}
            >
              <Form.Item
                name="username"
                label={t("auth.forgotPassword.step1.username")}
                rules={[{ required: true }]}
              >
                <Input autoComplete="username" autoFocus />
              </Form.Item>
              <Form.Item>
                <Button type="primary" htmlType="submit" loading={submitting} block>
                  {t("auth.forgotPassword.step1.submit")}
                </Button>
              </Form.Item>
            </Form>
          </>
        )}

        {step === "code" && (
          <>
            <Alert
              style={{ marginBottom: 16 }}
              type="info"
              showIcon
              message={t("auth.forgotPassword.step2.description")}
            />
            <Form<CodeFormValues>
              layout="vertical"
              onFinish={handleCodeFinish}
              disabled={submitting}
            >
              <Form.Item
                name="code"
                label={t("auth.forgotPassword.step2.code")}
                rules={[{ required: true, len: 6 }]}
              >
                <Input inputMode="numeric" maxLength={6} autoFocus autoComplete="one-time-code" />
              </Form.Item>
              <Form.Item>
                <Button type="primary" htmlType="submit" loading={submitting} block>
                  {t("auth.forgotPassword.step2.submit")}
                </Button>
              </Form.Item>
            </Form>
          </>
        )}

        {step === "password" && (
          <Form<PasswordFormValues>
            layout="vertical"
            onFinish={handlePasswordFinish}
            disabled={submitting}
          >
            <Form.Item
              name="newPassword"
              label={t("auth.forgotPassword.step3.newPassword")}
              rules={[{ required: true, min: 8, max: 128 }]}
            >
              <Input.Password autoComplete="new-password" autoFocus />
            </Form.Item>
            <Form.Item
              name="confirmPassword"
              label={t("auth.forgotPassword.step3.confirmPassword")}
              dependencies={["newPassword"]}
              rules={[
                { required: true },
                ({ getFieldValue }) => ({
                  validator(_rule, value: string) {
                    if (!value || getFieldValue("newPassword") === value) {
                      return Promise.resolve();
                    }
                    return Promise.reject(
                      new Error(t("auth.forgotPassword.errors.confirmMismatch")),
                    );
                  },
                }),
              ]}
            >
              <Input.Password autoComplete="new-password" />
            </Form.Item>
            <Form.Item>
              <Button type="primary" htmlType="submit" loading={submitting} block>
                {t("auth.forgotPassword.step3.submit")}
              </Button>
            </Form.Item>
          </Form>
        )}

        <Button type="link" block onClick={() => navigate({ to: "/login" })}>
          {t("auth.forgotPassword.backToLogin")}
        </Button>
      </Card>
    </div>
  );
}
