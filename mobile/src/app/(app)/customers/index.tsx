import { useRouter } from "expo-router";
import { Search, UserPlus } from "lucide-react-native";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { ActivityIndicator, FlatList, Pressable, TextInput, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";

import { Button } from "@/components/ui/button";
import { Text } from "@/components/ui/text";
import { useDebouncedValue } from "@/features/catalog/hooks";
import type { Customer } from "@/features/customers/api";
import { useCustomersSearch } from "@/features/customers/hooks";

const SEARCH_DEBOUNCE_MS = 300;

function CustomerRow({ customer, onPress }: { customer: Customer; onPress: () => void }) {
  return (
    <Pressable
      accessibilityRole="button"
      className="min-h-14 justify-center rounded-md border border-border bg-card p-3 active:bg-accent"
      onPress={onPress}
    >
      <Text numberOfLines={1}>{customer.fullName}</Text>
      {customer.phone ? (
        <Text variant="muted" numberOfLines={1}>
          {customer.phone}
        </Text>
      ) : null}
    </Pressable>
  );
}

/**
 * Customers list (T4 deliverable 3): search by name/phone, cursor-
 * paginated, plus an "Add customer" entry to `customers/new.tsx` — every
 * `cashier+` role may browse and create (`docs/04-DATA-MODEL.md` § 7).
 * `insets.bottom` (D-95) pads the list — see `customers/[id].tsx`'s doc
 * comment for why this stack needs it now that it's a drawer item (T12)
 * rather than a bottom tab.
 */
export default function CustomersScreen() {
  const { t } = useTranslation();
  const router = useRouter();
  const insets = useSafeAreaInsets();
  const [rawQuery, setRawQuery] = useState("");
  const debouncedQuery = useDebouncedValue(rawQuery, SEARCH_DEBOUNCE_MS);

  const {
    data,
    isPending,
    isError,
    isRefetching,
    refetch,
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
  } = useCustomersSearch(debouncedQuery);

  const customers = useMemo(() => data?.pages.flatMap((page) => page.items) ?? [], [data]);

  return (
    <View className="flex-1 bg-background">
      <View className="mx-4 mt-4 flex-row items-center gap-2">
        <View className="h-12 flex-1 flex-row items-center gap-2 rounded-md border border-input bg-background px-3">
          <Search color="#71717a" size={18} />
          <TextInput
            className="flex-1 text-base text-foreground"
            placeholder={t("customers.searchPlaceholder")}
            value={rawQuery}
            onChangeText={setRawQuery}
            autoCorrect={false}
            accessibilityLabel={t("customers.searchPlaceholder")}
          />
        </View>
        <Button
          size="icon"
          onPress={() => router.push("/customers/new")}
          accessibilityLabel={t("customers.add")}
        >
          <UserPlus size={18} color="white" />
        </Button>
      </View>

      {isPending ? (
        <View className="flex-1 items-center justify-center">
          <ActivityIndicator />
        </View>
      ) : isError ? (
        <View className="flex-1 items-center justify-center gap-2 p-6">
          <Text variant="muted">{t("errors.generic")}</Text>
          <Pressable accessibilityRole="button" onPress={() => refetch()}>
            <Text className="text-primary">{t("common.retry")}</Text>
          </Pressable>
        </View>
      ) : (
        <FlatList
          className="flex-1 px-4"
          contentContainerStyle={{ paddingTop: 12, paddingBottom: insets.bottom + 12, gap: 8 }}
          data={customers}
          keyExtractor={(customer) => customer.id}
          refreshing={isRefetching}
          onRefresh={refetch}
          onEndReachedThreshold={0.4}
          onEndReached={() => {
            if (hasNextPage && !isFetchingNextPage) {
              fetchNextPage();
            }
          }}
          ListEmptyComponent={
            <Text variant="muted" className="p-4 text-center">
              {t("mobile.customers.list.empty")}
            </Text>
          }
          ListFooterComponent={isFetchingNextPage ? <ActivityIndicator className="py-4" /> : null}
          renderItem={({ item: customer }) => (
            <CustomerRow
              customer={customer}
              onPress={() => router.push(`/customers/${customer.id}`)}
            />
          )}
        />
      )}
    </View>
  );
}
