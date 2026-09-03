import { resources } from "@savdo/i18n";
import { useQuery } from "@tanstack/react-query";
import { ActivityIndicator, Text, View } from "react-native";

import { api } from "@/lib/api";
import { healthLabel } from "@/lib/healthLabel";

async function fetchHealthz() {
  const { data, error } = await api.GET("/healthz");
  if (error) {
    throw error;
  }
  return data;
}

/** Phase 0 hello: proves the mobile app can reach the API through the
 * generated client and render an i18n string with NativeWind classes. */
export default function HomeScreen() {
  const { data, isPending, isError } = useQuery({
    queryKey: ["healthz"],
    queryFn: fetchHealthz,
  });

  return (
    <View className="flex-1 items-center justify-center gap-3 bg-white px-6">
      <Text className="text-2xl font-bold text-slate-900">{resources.uz.app.name}</Text>
      {isPending && <ActivityIndicator />}
      {isError && <Text className="text-red-600">{resources.uz.common.error}</Text>}
      {data && <Text className="text-base text-slate-700">{healthLabel(data.status)}</Text>}
    </View>
  );
}
