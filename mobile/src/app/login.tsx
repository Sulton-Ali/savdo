import { tokens } from "@savdo/ui-tokens";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Controller, useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { ActivityIndicator, KeyboardAvoidingView, Platform, ScrollView, View } from "react-native";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Text } from "@/components/ui/text";
import { ApiAuthError } from "@/lib/authApi";
import { TOKEN_QUERY_KEY } from "@/lib/queryKeys";
import { getServerUrl, isValidServerUrl, setServerUrl } from "@/lib/serverUrl";
import { useLogin } from "@/lib/session";
import { clearToken } from "@/lib/token";

interface LoginFormValues {
  server: string;
  username: string;
  password: string;
}

/** Translation key for `auth.errors.*`, chosen from the API's `ErrorCode`
 * (ADR-013) — a raw API sentence is never shown. Mirrors `admin`'s
 * `LoginPage.tsx`. */
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

export default function LoginScreen() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const login = useLogin();
  const [errorKey, setErrorKey] = useState<string | null>(null);
  const [retryAfterSeconds, setRetryAfterSeconds] = useState<number | null>(null);

  const {
    control,
    handleSubmit,
    formState: { errors, isLoading, isSubmitting },
  } = useForm<LoginFormValues>({
    // react-hook-form runs an async `defaultValues` function and exposes its
    // pending state as `formState.isLoading` — no separate query needed to
    // prefill the "Server" field from SecureStore (D-79).
    defaultValues: async () => ({
      server: await getServerUrl(),
      username: "",
      password: "",
    }),
  });

  async function onSubmit(values: LoginFormValues) {
    setErrorKey(null);
    setRetryAfterSeconds(null);

    // Changing the server address invalidates any token from the old one
    // (D-79) — the reactive auth gate in `_layout.tsx` reads this same
    // query key, so clearing it here is enough, no navigation call needed.
    const currentServerUrl = await getServerUrl();
    if (values.server !== currentServerUrl) {
      await setServerUrl(values.server);
      await clearToken();
      queryClient.setQueryData(TOKEN_QUERY_KEY, null);
    }

    try {
      await login.mutateAsync({ username: values.username, password: values.password });
    } catch (error) {
      setErrorKey(errorKeyFor(error));
      setRetryAfterSeconds(
        error instanceof ApiAuthError ? (error.retryAfterSeconds ?? null) : null,
      );
    }
  }

  return (
    <KeyboardAvoidingView
      behavior={Platform.OS === "ios" ? "padding" : undefined}
      className="flex-1 bg-background"
    >
      <ScrollView contentContainerStyle={{ flexGrow: 1 }} keyboardShouldPersistTaps="handled">
        <View className="flex-1 items-center justify-center p-6">
          <Card className="w-full max-w-sm">
            <CardHeader>
              <CardTitle>{t("auth.login.title")}</CardTitle>
            </CardHeader>
            <CardContent className="gap-4">
              {errorKey && (
                <View className="rounded-md bg-destructive/10 p-3">
                  <Text className="text-destructive">{t(`auth.errors.${errorKey}`)}</Text>
                  {retryAfterSeconds != null && (
                    <Text variant="small" className="text-destructive">
                      {t("auth.errors.retryAfter", { seconds: retryAfterSeconds })}
                    </Text>
                  )}
                </View>
              )}

              <View className="gap-1.5">
                <Text variant="small">{t("mobile.shell.server")}</Text>
                <Controller
                  control={control}
                  name="server"
                  rules={{
                    required: t("errors.field.required"),
                    validate: (value) => isValidServerUrl(value) || t("errors.field.invalid"),
                  }}
                  render={({ field }) => (
                    <Input
                      value={field.value}
                      onChangeText={field.onChange}
                      onBlur={field.onBlur}
                      autoCapitalize="none"
                      autoCorrect={false}
                      keyboardType="url"
                      placeholder="http://10.0.2.2:8080/v1"
                      editable={!isLoading}
                    />
                  )}
                />
                {errors.server && (
                  <Text variant="small" className="text-destructive">
                    {errors.server.message}
                  </Text>
                )}
              </View>

              <View className="gap-1.5">
                <Text variant="small">{t("auth.login.username")}</Text>
                <Controller
                  control={control}
                  name="username"
                  rules={{ required: t("errors.field.required") }}
                  render={({ field }) => (
                    <Input
                      value={field.value}
                      onChangeText={field.onChange}
                      onBlur={field.onBlur}
                      autoCapitalize="none"
                      autoCorrect={false}
                      autoComplete="username"
                      editable={!isLoading}
                    />
                  )}
                />
                {errors.username && (
                  <Text variant="small" className="text-destructive">
                    {errors.username.message}
                  </Text>
                )}
              </View>

              <View className="gap-1.5">
                <Text variant="small">{t("auth.login.password")}</Text>
                <Controller
                  control={control}
                  name="password"
                  rules={{ required: t("errors.field.required") }}
                  render={({ field }) => (
                    <Input
                      value={field.value}
                      onChangeText={field.onChange}
                      onBlur={field.onBlur}
                      secureTextEntry
                      autoComplete="current-password"
                      editable={!isLoading}
                    />
                  )}
                />
                {errors.password && (
                  <Text variant="small" className="text-destructive">
                    {errors.password.message}
                  </Text>
                )}
              </View>

              <Button disabled={isLoading || isSubmitting} onPress={handleSubmit(onSubmit)}>
                {isSubmitting ? (
                  <ActivityIndicator color={tokens.color.surface} />
                ) : (
                  <Text>{t("auth.login.submit")}</Text>
                )}
              </Button>
            </CardContent>
          </Card>
        </View>
      </ScrollView>
    </KeyboardAvoidingView>
  );
}
