import { useLocalSearchParams, useRouter } from "expo-router";
import { useState } from "react";
import { Controller, useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { KeyboardAvoidingView, Platform, ScrollView, View } from "react-native";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Text } from "@/components/ui/text";
import { CustomersApiError } from "@/features/customers/api";
import { useCreateCustomer } from "@/features/customers/hooks";

interface NewCustomerFormValues {
  fullName: string;
  phone: string;
}

/**
 * Create a customer (T4 deliverable 3), `POST /customers`. Reached either
 * from the Customers tab's own "Add customer" button (`customers/index.tsx`,
 * no `returnTo` — lands on the new customer's detail) or from the Sale
 * screen's customer row (`sale/index.tsx`, `?returnTo=sale`) — on success
 * in that case, this screen hands the created customer back as three route
 * params (`attachCustomerId`/`attachCustomerName`/`attachCustomerPhone`)
 * rather than a second `GET /customers/{id}` round trip, since
 * `createCustomer`'s own response already has everything the sale screen's
 * customer row needs to display. `fullName` is the only field
 * `CustomerCreate` requires; `phone` is required by this screen's own form
 * instead (not the contract) since the point of reaching it from a sale is
 * specifically to attach a customer *by phone* — flagged in this task's
 * report as a product choice, not an API constraint.
 */
export default function NewCustomerScreen() {
  const { t } = useTranslation();
  const router = useRouter();
  const { returnTo } = useLocalSearchParams<{ returnTo?: string }>();
  const createCustomer = useCreateCustomer();
  const [duplicatePhone, setDuplicatePhone] = useState(false);
  const [generalError, setGeneralError] = useState<string | null>(null);

  const {
    control,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<NewCustomerFormValues>({ defaultValues: { fullName: "", phone: "" } });

  async function onSubmit(values: NewCustomerFormValues) {
    setDuplicatePhone(false);
    setGeneralError(null);
    try {
      const customer = await createCustomer.mutateAsync({
        fullName: values.fullName.trim(),
        phone: values.phone.trim(),
      });
      if (returnTo === "sale") {
        // This screen was pushed onto the *Customers* tab's own stack
        // (its file lives under `app/(app)/customers/`, regardless of
        // which tab's button pushed it): `replace` swaps this stack
        // entry for `/sale` and switches the active tab there, which is
        // what the round trip needs. A `router.dismissTo` alternative
        // was tried here to also clear the Customers tab's own history
        // (so switching back to it later shows its list, not this stale
        // form) but did not navigate at all when tested on-device
        // (T4's smoke test) — reverted to this known-working `replace`;
        // the stale-Customers-tab-history nit is left for a follow-up,
        // noted in this task's report.
        router.replace({
          pathname: "/sale",
          params: {
            attachCustomerId: customer.id,
            attachCustomerName: customer.fullName,
            attachCustomerPhone: customer.phone ?? "",
          },
        });
        return;
      }
      router.replace(`/customers/${customer.id}`);
    } catch (error) {
      // Every error keeps the form (with whatever was typed) so the
      // cashier can fix it and retry — only `CONFLICT` (a duplicate
      // phone) gets its own field-level message; everything else
      // (validation, a network drop, a 5xx) gets a generic translated
      // banner instead of silently doing nothing (T4 review nit).
      if (error instanceof CustomersApiError && error.code === "CONFLICT") {
        setDuplicatePhone(true);
        return;
      }
      setGeneralError(t("errors.generic"));
    }
  }

  return (
    <KeyboardAvoidingView
      behavior={Platform.OS === "ios" ? "padding" : undefined}
      className="flex-1 bg-background"
    >
      <ScrollView
        contentContainerStyle={{ padding: 16, gap: 16 }}
        keyboardShouldPersistTaps="handled"
      >
        {returnTo === "sale" ? (
          <Text variant="muted">{t("mobile.customers.new.forSaleHint")}</Text>
        ) : null}

        {generalError ? (
          <View className="rounded-md bg-destructive/10 p-3">
            <Text className="text-destructive">{generalError}</Text>
          </View>
        ) : null}

        <View className="gap-1.5">
          <Text variant="small">{t("customers.fields.fullName")}</Text>
          <Controller
            control={control}
            name="fullName"
            rules={{ required: t("errors.field.required") }}
            render={({ field }) => (
              <Input
                value={field.value}
                onChangeText={field.onChange}
                onBlur={field.onBlur}
                autoCorrect={false}
              />
            )}
          />
          {errors.fullName ? (
            <Text variant="small" className="text-destructive">
              {errors.fullName.message}
            </Text>
          ) : null}
        </View>

        <View className="gap-1.5">
          <Text variant="small">{t("customers.fields.phone")}</Text>
          <Controller
            control={control}
            name="phone"
            rules={{
              required: t("errors.field.required"),
              // `required` alone only rejects an empty string — a
              // whitespace-only value (e.g. a stray space) would pass it
              // and then get trimmed to "" right before the request,
              // sending a blank phone silently (T4 review nit 12).
              validate: (value) => (value.trim().length > 0 ? true : t("errors.field.required")),
            }}
            render={({ field }) => (
              <Input
                value={field.value}
                onChangeText={field.onChange}
                onBlur={field.onBlur}
                keyboardType="phone-pad"
                autoCorrect={false}
              />
            )}
          />
          {errors.phone ? (
            <Text variant="small" className="text-destructive">
              {errors.phone.message}
            </Text>
          ) : null}
          {duplicatePhone ? (
            <Text variant="small" className="text-destructive">
              {t("mobile.customers.new.duplicatePhone")}
            </Text>
          ) : null}
        </View>

        <Button disabled={isSubmitting} onPress={handleSubmit(onSubmit)}>
          <Text>{t("common.save")}</Text>
        </Button>
      </ScrollView>
    </KeyboardAvoidingView>
  );
}
