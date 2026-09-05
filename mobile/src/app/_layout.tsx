import "../i18n";
import "../global.css";

import { tokens } from "@savdo/ui-tokens";
import { QueryClientProvider } from "@tanstack/react-query";
import { Stack } from "expo-router";
import { StatusBar } from "expo-status-bar";
import { ActivityIndicator, View } from "react-native";
import { SafeAreaProvider } from "react-native-safe-area-context";

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
 * are loading, show a spinner rather than guessing which stack to mount.
 * Once resolved, `Stack.Protected` shows exactly one of `(app)` or `login`
 * and stays in sync as the session changes — login, logout, or a 401 bounce
 * from `lib/api.ts`'s middleware.
 */
function RootNavigator() {
  const { isLoading, isAuthenticated } = useSession();

  if (isLoading) {
    return (
      <View className="flex-1 items-center justify-center bg-background">
        <ActivityIndicator color={tokens.color.primary} />
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
