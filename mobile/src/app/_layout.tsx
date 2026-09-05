import "../i18n";
import "../global.css";

import { tokens } from "@savdo/ui-tokens";
import { QueryClientProvider } from "@tanstack/react-query";
import { router, Stack } from "expo-router";
import { StatusBar } from "expo-status-bar";
import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { ActivityIndicator, View } from "react-native";
import { SafeAreaProvider } from "react-native-safe-area-context";

import { Button } from "@/components/ui/button";
import { Text } from "@/components/ui/text";
import { queryClient } from "@/lib/queryClient";
import { useSession } from "@/lib/session";

export default function RootLayout() {
  return (
    <QueryClientProvider client={queryClient}>
      <SafeAreaProvider>
        <StatusBar style="auto" />
        <RootNavigator />
      </SafeAreaProvider>
    </QueryClientProvider>
  );
}

/**
 * Auth gate (D-29): while the SecureStore-backed token and `GET /auth/me`
 * are loading, show a spinner rather than guessing which stack to mount. If
 * a token exists but `/auth/me` is unreachable (offline, a 5xx) after its
 * retries, that's an outage, not a logout — show a retry screen and keep
 * the token, instead of bouncing a still-valid session to the login form.
 *
 * `Stack.Protected` alone only controls which screen a *new* navigation can
 * land on — verified on-device it does not force-unmount an already-active,
 * deeply nested screen (e.g. a non-index tab) when its guard flips to false
 * while mounted, so logging out (or a 401 bounce) while inside a tab left
 * the stale authenticated screen on top with no way back. The effect below
 * explicitly steps in for that live transition; `lib/api.ts`'s 401
 * middleware itself still never navigates (it only clears the session), so
 * this is the one place that does.
 */
function RootNavigator() {
  const { t } = useTranslation();
  const { isLoading, isUnreachable, isAuthenticated, retry } = useSession();

  useEffect(() => {
    if (!isLoading && !isUnreachable && !isAuthenticated) {
      router.replace("/login");
    }
  }, [isLoading, isUnreachable, isAuthenticated]);

  if (isLoading) {
    return (
      <View className="flex-1 items-center justify-center bg-background">
        <ActivityIndicator color={tokens.color.primary} />
      </View>
    );
  }

  if (isUnreachable) {
    return (
      <View className="flex-1 items-center justify-center gap-4 bg-background p-6">
        <Text className="text-center">{t("errors.serviceUnavailable")}</Text>
        <Button onPress={retry}>
          <Text>{t("common.retry")}</Text>
        </Button>
      </View>
    );
  }

  return (
    <Stack screenOptions={{ headerShown: false }}>
      <Stack.Protected guard={isAuthenticated}>
        <Stack.Screen name="(app)" />
      </Stack.Protected>
      <Stack.Protected guard={!isAuthenticated}>
        <Stack.Screen name="login" />
      </Stack.Protected>
    </Stack>
  );
}
