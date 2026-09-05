import { tokens } from "@savdo/ui-tokens";
import * as ImagePicker from "expo-image-picker";
import { Redirect, useLocalSearchParams } from "expo-router";
import type { TFunction } from "i18next";
import { useState } from "react";
import { Controller, useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { ActivityIndicator, Image, Pressable, ScrollView, Switch, View } from "react-native";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Text } from "@/components/ui/text";
import type { Product, Variant } from "@/features/catalog/api";
import { CatalogEditApiError, type ProductImage } from "@/features/catalog/edit/api";
import {
  buildProductPatch,
  buildVariantPatch,
  isValidDecimal,
  isValidLowStockThreshold,
  type ProductFormValues,
  productToFormValues,
  type VariantFormValues,
  validatePromoFields,
  variantToFormValues,
} from "@/features/catalog/edit/form";
import {
  useRemoveProductImage,
  useUpdateProduct,
  useUpdateProductImage,
  useUpdateVariant,
  useUploadProductImage,
} from "@/features/catalog/edit/hooks";
import { useMediaUrl, useProduct } from "@/features/catalog/hooks";
import { useSession } from "@/lib/session";

const MAX_PRODUCT_IMAGES = 8;

/** Translates a thrown `CatalogEditApiError` (or anything else) into a
 * display sentence (ADR-013) — mirrors `admin/src/routes/app/
 * ImageGallery.tsx`'s `handleImageError` for the codes this screen's
 * mutations can actually return. */
function errorMessageFor(t: TFunction, error: unknown): string {
  if (error instanceof CatalogEditApiError) {
    if (error.code === "RATE_LIMITED") {
      return error.retryAfterSeconds != null
        ? t("mobile.catalogEdit.errors.retryAfter", { seconds: error.retryAfterSeconds })
        : t("catalog.images.tooManyUploads");
    }
    if (error.code === "VALIDATION_FAILED") {
      const fields = (error.details as { fields?: Record<string, string> }).fields ?? {};
      if (fields.mediaId === "invalid") {
        return t("catalog.images.capReached");
      }
    }
    if (error.code === "CONFLICT") {
      const field = (error.details as { field?: string }).field;
      if (field === "mediaId") {
        return t("catalog.images.alreadyAttached");
      }
    }
    if (error.code === "FORBIDDEN") {
      return t("errors.forbidden");
    }
    if (error.code === "NOT_FOUND") {
      return t("errors.notFound");
    }
  }
  return t("errors.generic");
}

function ErrorBanner({ message }: { message: string }) {
  return (
    <View className="rounded-md bg-destructive/10 p-3">
      <Text className="text-destructive">{message}</Text>
    </View>
  );
}

/**
 * Product quick edit (D-77): manager/owner only (`catalog.write`, D-81) —
 * a cashier reaching this URL is bounced back to the read-only product
 * screen. Pushed within `products/_layout.tsx`'s Stack, so the header
 * (title, back button) is native.
 */
export default function ProductEditScreen() {
  const { t } = useTranslation();
  const { id } = useLocalSearchParams<{ id: string }>();
  const { can } = useSession();
  const canEdit = can("catalog.write");
  const productQuery = useProduct(id);

  if (!canEdit) {
    return <Redirect href={`/products/${id}`} />;
  }

  if (productQuery.isPending) {
    return (
      <View className="flex-1 items-center justify-center bg-background">
        <ActivityIndicator />
      </View>
    );
  }

  if (productQuery.isError || !productQuery.data) {
    return (
      <View className="flex-1 items-center justify-center gap-2 bg-background p-6">
        <Text variant="muted">{t("errors.notFound")}</Text>
      </View>
    );
  }

  return (
    <ScrollView className="flex-1 bg-background" contentContainerStyle={{ padding: 16, gap: 20 }}>
      <ProductFields product={productQuery.data} />
      <VariantsSection productId={id} variants={productQuery.data.variants ?? []} />
      <ImagesSection productId={id} images={productQuery.data.images ?? []} />
    </ScrollView>
  );
}

function promoErrorMessage(t: TFunction, reason: ReturnType<typeof validatePromoFields>): string {
  switch (reason) {
    case "incomplete":
      return t("mobile.catalogEdit.errors.promoIncomplete");
    case "invalidPrice":
      return t("mobile.catalogEdit.errors.invalidPrice");
    case "invalidDate":
      return t("mobile.catalogEdit.errors.promoInvalidDate");
    case "rangeInvalid":
      return t("mobile.catalogEdit.errors.promoRangeInvalid");
    default:
      return "";
  }
}

/** Price, promo, active flag and low-stock threshold — one `PATCH
 * /products/{id}` per Save (deliverable 3). Never renders or sends
 * `costPrice` (ADR-010 hard rule 4 — out of this task's scope even for a
 * manager who could see it in the response). */
function ProductFields({ product }: { product: Product }) {
  const { t } = useTranslation();
  const updateProductMutation = useUpdateProduct(product.id);
  const [banner, setBanner] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);

  const {
    control,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<ProductFormValues>({ defaultValues: productToFormValues(product) });

  async function onSubmit(values: ProductFormValues) {
    setBanner(null);
    setSaved(false);
    const patch = buildProductPatch(product, values);
    if (Object.keys(patch).length === 0) {
      return;
    }
    try {
      await updateProductMutation.mutateAsync(patch);
      setSaved(true);
    } catch (error) {
      setBanner(errorMessageFor(t, error));
    }
  }

  return (
    <View className="gap-4">
      {banner && <ErrorBanner message={banner} />}
      {saved && (
        <Text variant="small" className="text-muted-foreground">
          {t("catalog.products.saved")}
        </Text>
      )}

      <View className="gap-1.5">
        <Text variant="small">{t("catalog.products.fields.basePrice")}</Text>
        <Controller
          control={control}
          name="basePrice"
          rules={{
            required: t("errors.field.required"),
            validate: (value) =>
              isValidDecimal(value) || t("mobile.catalogEdit.errors.invalidPrice"),
          }}
          render={({ field }) => (
            <Input
              value={field.value}
              onChangeText={field.onChange}
              onBlur={field.onBlur}
              keyboardType="decimal-pad"
            />
          )}
        />
        {errors.basePrice && (
          <Text variant="small" className="text-destructive">
            {errors.basePrice.message}
          </Text>
        )}
      </View>

      <View className="gap-1.5">
        <Text variant="small">{t("catalog.products.fields.promoPrice")}</Text>
        <Controller
          control={control}
          name="promoPrice"
          rules={{
            validate: (_value, formValues) => {
              const reason = validatePromoFields(formValues);
              return reason ? promoErrorMessage(t, reason) : true;
            },
          }}
          render={({ field }) => (
            <Input
              value={field.value}
              onChangeText={field.onChange}
              onBlur={field.onBlur}
              keyboardType="decimal-pad"
            />
          )}
        />
      </View>

      <View className="flex-row gap-3">
        <View className="flex-1 gap-1.5">
          <Text variant="small">{t("mobile.catalogEdit.fields.promoFrom")}</Text>
          <Controller
            control={control}
            name="promoFrom"
            rules={{
              validate: (_value, formValues) => {
                const reason = validatePromoFields(formValues);
                return reason ? promoErrorMessage(t, reason) : true;
              },
            }}
            render={({ field }) => (
              <Input
                value={field.value}
                onChangeText={field.onChange}
                onBlur={field.onBlur}
                placeholder="YYYY-MM-DD"
                autoCapitalize="none"
              />
            )}
          />
        </View>
        <View className="flex-1 gap-1.5">
          <Text variant="small">{t("mobile.catalogEdit.fields.promoTo")}</Text>
          <Controller
            control={control}
            name="promoTo"
            rules={{
              validate: (_value, formValues) => {
                const reason = validatePromoFields(formValues);
                return reason ? promoErrorMessage(t, reason) : true;
              },
            }}
            render={({ field }) => (
              <Input
                value={field.value}
                onChangeText={field.onChange}
                onBlur={field.onBlur}
                placeholder="YYYY-MM-DD"
                autoCapitalize="none"
              />
            )}
          />
        </View>
      </View>
      {(errors.promoPrice || errors.promoFrom || errors.promoTo) && (
        <Text variant="small" className="text-destructive">
          {(errors.promoPrice ?? errors.promoFrom ?? errors.promoTo)?.message}
        </Text>
      )}
      <Text variant="small" className="text-muted-foreground">
        {t("mobile.catalogEdit.fields.promoDateHint")}
      </Text>

      <View className="flex-row items-center justify-between">
        <Text>{t("catalog.products.fields.isActive")}</Text>
        <Controller
          control={control}
          name="isActive"
          render={({ field }) => <Switch value={field.value} onValueChange={field.onChange} />}
        />
      </View>

      <View className="gap-1.5">
        <Text variant="small">{t("catalog.products.fields.lowStockThreshold")}</Text>
        <Controller
          control={control}
          name="lowStockThreshold"
          rules={{
            validate: (value) =>
              isValidLowStockThreshold(value) || t("mobile.catalogEdit.errors.invalidThreshold"),
          }}
          render={({ field }) => (
            <Input
              value={field.value}
              onChangeText={field.onChange}
              onBlur={field.onBlur}
              keyboardType="number-pad"
            />
          )}
        />
        {errors.lowStockThreshold && (
          <Text variant="small" className="text-destructive">
            {errors.lowStockThreshold.message}
          </Text>
        )}
        <Text variant="small" className="text-muted-foreground">
          {t("catalog.products.lowStockThresholdHint")}
        </Text>
      </View>

      <Button disabled={isSubmitting} onPress={handleSubmit(onSubmit)}>
        {isSubmitting ? (
          <ActivityIndicator color={tokens.color.surface} />
        ) : (
          <Text>{t("common.save")}</Text>
        )}
      </Button>
    </View>
  );
}

function variantLabel(variant: Variant): string {
  const attrs = Object.entries(variant.attributes)
    .map(([key, value]) => `${key}: ${value}`)
    .join(", ");
  return attrs || variant.sku || variant.id;
}

function VariantsSection({ productId, variants }: { productId: string; variants: Variant[] }) {
  const { t } = useTranslation();
  if (variants.length === 0) {
    return null;
  }
  return (
    <View className="gap-3">
      <Text variant="large">{t("catalog.products.tabs.variants")}</Text>
      {variants.map((variant) => (
        <VariantRow key={variant.id} productId={productId} variant={variant} />
      ))}
    </View>
  );
}

/** One variant's price override and active flag, saved independently of
 * every other row and of the product-level Save above (deliverable 3: "a
 * row Save"). */
function VariantRow({ productId, variant }: { productId: string; variant: Variant }) {
  const { t } = useTranslation();
  const updateVariantMutation = useUpdateVariant(productId);
  const [rowError, setRowError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);

  const {
    control,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<VariantFormValues>({ defaultValues: variantToFormValues(variant) });

  async function onSubmit(values: VariantFormValues) {
    setRowError(null);
    setSaved(false);
    const patch = buildVariantPatch(variant, values);
    if (Object.keys(patch).length === 0) {
      return;
    }
    try {
      await updateVariantMutation.mutateAsync({ variantId: variant.id, patch });
      setSaved(true);
    } catch (error) {
      setRowError(errorMessageFor(t, error));
    }
  }

  return (
    <View className="gap-2 rounded-md border border-border p-3">
      <Text>{variantLabel(variant)}</Text>
      {rowError && <ErrorBanner message={rowError} />}
      {saved && (
        <Text variant="small" className="text-muted-foreground">
          {t("catalog.variants.saved")}
        </Text>
      )}

      <View className="gap-1.5">
        <Text variant="small">{t("catalog.variants.fields.priceOverride")}</Text>
        <Controller
          control={control}
          name="priceOverride"
          rules={{
            validate: (value) =>
              value.trim() === "" ||
              isValidDecimal(value) ||
              t("mobile.catalogEdit.errors.invalidPrice"),
          }}
          render={({ field }) => (
            <Input
              value={field.value}
              onChangeText={field.onChange}
              onBlur={field.onBlur}
              keyboardType="decimal-pad"
              placeholder={t("catalog.products.fields.basePrice")}
            />
          )}
        />
        {errors.priceOverride && (
          <Text variant="small" className="text-destructive">
            {errors.priceOverride.message}
          </Text>
        )}
      </View>

      <View className="flex-row items-center justify-between">
        <Text variant="small">{t("catalog.variants.fields.isActive")}</Text>
        <Controller
          control={control}
          name="isActive"
          render={({ field }) => <Switch value={field.value} onValueChange={field.onChange} />}
        />
      </View>

      <Button size="sm" variant="outline" disabled={isSubmitting} onPress={handleSubmit(onSubmit)}>
        {isSubmitting ? <ActivityIndicator /> : <Text>{t("common.save")}</Text>}
      </Button>
    </View>
  );
}

function ImagesSection({ productId, images }: { productId: string; images: ProductImage[] }) {
  const { t } = useTranslation();
  const sorted = [...images].sort((a, b) => a.sortOrder - b.sortOrder);
  const atCap = sorted.length >= MAX_PRODUCT_IMAGES;
  const uploadMutation = useUploadProductImage(productId);
  const [banner, setBanner] = useState<string | null>(null);

  async function handlePick(source: "camera" | "library") {
    setBanner(null);
    const permission =
      source === "camera"
        ? await ImagePicker.requestCameraPermissionsAsync()
        : await ImagePicker.requestMediaLibraryPermissionsAsync();
    if (!permission.granted) {
      setBanner(
        t(
          source === "camera"
            ? "mobile.catalogEdit.errors.cameraPermissionDenied"
            : "mobile.catalogEdit.errors.libraryPermissionDenied",
        ),
      );
      return;
    }

    const result =
      source === "camera"
        ? await ImagePicker.launchCameraAsync({ quality: 0.8, allowsEditing: false })
        : await ImagePicker.launchImageLibraryAsync({ quality: 0.8, allowsEditing: false });
    if (result.canceled) {
      return;
    }
    const asset = result.assets[0];
    if (!asset) {
      return;
    }

    try {
      await uploadMutation.mutateAsync({
        uri: asset.uri,
        mimeType: asset.mimeType ?? "image/jpeg",
        fileName: asset.fileName ?? `photo-${Date.now()}.jpg`,
        isCover: sorted.length === 0,
      });
    } catch (error) {
      setBanner(errorMessageFor(t, error));
    }
  }

  return (
    <View className="gap-3">
      <Text variant="large">{t("catalog.images.title")}</Text>
      {banner && <ErrorBanner message={banner} />}

      {sorted.length > 0 && (
        <View className="flex-row flex-wrap gap-3">
          {sorted.map((image) => (
            <ImageCard key={image.id} productId={productId} image={image} onError={setBanner} />
          ))}
        </View>
      )}

      <Text variant="small" className="text-muted-foreground">
        {atCap ? t("catalog.images.capReached") : t("catalog.images.uploadHint")}
      </Text>

      <View className="flex-row gap-2">
        <Button
          variant="outline"
          disabled={atCap || uploadMutation.isPending}
          onPress={() => handlePick("camera")}
        >
          {uploadMutation.isPending ? (
            <ActivityIndicator />
          ) : (
            <Text>{t("mobile.catalogEdit.actions.takePhoto")}</Text>
          )}
        </Button>
        <Button
          variant="outline"
          disabled={atCap || uploadMutation.isPending}
          onPress={() => handlePick("library")}
        >
          <Text>{t("mobile.catalogEdit.actions.choosePhoto")}</Text>
        </Button>
      </View>
    </View>
  );
}

function ImageCard({
  productId,
  image,
  onError,
}: {
  productId: string;
  image: ProductImage;
  onError: (message: string) => void;
}) {
  const { t } = useTranslation();
  const url = useMediaUrl(image.urls.card);
  const setCoverMutation = useUpdateProductImage(productId);
  const removeMutation = useRemoveProductImage(productId);

  function handleSetCover() {
    setCoverMutation.mutate(
      { imageId: image.id, patch: { isCover: true } },
      { onError: (error) => onError(errorMessageFor(t, error)) },
    );
  }

  function handleRemove() {
    removeMutation.mutate(image.id, { onError: (error) => onError(errorMessageFor(t, error)) });
  }

  return (
    <View className="w-28 gap-1">
      {url ? (
        <Image
          source={{ uri: url }}
          style={{ width: 112, height: 112, borderRadius: 8 }}
          resizeMode="cover"
        />
      ) : (
        <View className="h-28 w-28 rounded-md bg-muted" />
      )}
      {image.isCover ? (
        <Text variant="small" className="text-muted-foreground">
          {t("catalog.images.cover")}
        </Text>
      ) : (
        <Pressable disabled={setCoverMutation.isPending} onPress={handleSetCover}>
          <Text variant="small" className="text-primary">
            {t("catalog.images.setCover")}
          </Text>
        </Pressable>
      )}
      <Pressable disabled={removeMutation.isPending} onPress={handleRemove}>
        <Text variant="small" className="text-destructive">
          {t("catalog.images.remove")}
        </Text>
      </Pressable>
    </View>
  );
}
