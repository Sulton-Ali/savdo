import "../i18n";
import "../global.css";

import { tokens } from "@savdo/ui-tokens";
import { QueryClientProvider } from "@tanstack/react-query";
import { router, Stack } from "expo-router";
import { StatusBar } from "expo-status-bar";
import { useEffect, useRef, useState } from "react";
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
 * a token exists but `me` has never loaded for it (offline, a 5xx, a token
 * bound to a LAN IP that's since gone dark, D-81), that's an outage, not a
 * logout — show a retry screen instead of bouncing a still-valid session to
 * the login form. That screen also offers an escape hatch ("change server /
 * log out"): without one, a token bound to an address that will never
 * answer again would strand the user on the outage screen forever, across
 * restarts, with the login screen — and its editable "Server" field, D-79 —
 * unreachable.
 *
 * `Stack.Protected` alone only controls which screen a *new* navigation can
 * land on — verified on-device it does not force-unmount an already-active,
 * deeply nested screen (e.g. a non-index tab) when its guard flips to false
 * while mounted, so logging out (or a 401 bounce) while inside a tab left
 * the stale authenticated screen on top with no way back. The effect below
 * explicitly steps in for that live authenticated→unauthenticated
 * transition (a ref guards against firing on cold start, when there was
 * never an authenticated screen to leave); `lib/api.ts`'s 401 middleware
 * itself still never navigates, it only clears the session.
 */
function RootNavigator() {
  const { t } = useTranslation();
  const { isLoading, isUnreachable, isAuthenticated, retry, escapeUnreachable } = useSession();
  const [isRetrying, setIsRetrying] = useState(false);
  const [isEscaping, setIsEscaping] = useState(false);
  const wasAuthenticatedRef = useRef(isAuthenticated);

  useEffect(() => {
    if (!isLoading && !isUnreachable && wasAuthenticatedRef.current && !isAuthenticated) {
      router.replace("/login");
    }
    wasAuthenticatedRef.current = isAuthenticated;
  }, [isLoading, isUnreachable, isAuthenticated]);

  async function handleRetry() {
    setIsRetrying(true);
    try {
      await retry();
    } finally {
      setIsRetrying(false);
    }
  }

  async function handleEscape() {
    setIsEscaping(true);
    try {
      await escapeUnreachable();
    } finally {
      setIsEscaping(false);
    }
  }

  if (isLoading) {
    return (
      <View className="flex-1 items-center justify-center bg-background">
        <ActivityIndicator color={tokens.color.primary} />
      </View>
    );
  }

  if (isUnreachable) {
    const busy = isRetrying || isEscaping;
    return (
      <View className="flex-1 items-center justify-center gap-4 bg-background p-6">
        <Text className="text-center">{t("errors.serviceUnavailable")}</Text>
        <Button onPress={handleRetry} disabled={busy}>
          {isRetrying ? (
            <ActivityIndicator color={tokens.color.surface} />
          ) : (
            <Text>{t("common.retry")}</Text>
          )}
        </Button>
        <Button variant="outline" onPress={handleEscape} disabled={busy}>
          {isEscaping ? (
            <ActivityIndicator color={tokens.color.primary} />
          ) : (
            <Text>{t("mobile.shell.outageEscape")}</Text>
          )}
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
