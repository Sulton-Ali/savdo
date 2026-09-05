import { useMutation, useQueryClient } from "@tanstack/react-query";

import { catalogKeys } from "@/lib/queryKeys";

import {
  addProductImage,
  type ProductImageCreate,
  type ProductImagePatch,
  type ProductPatch,
  removeProductImage,
  updateProduct,
  updateProductImage,
  updateVariant,
  uploadMedia,
  type VariantPatch,
} from "./api";

/**
 * Every mutation below changes exactly one product's detail (and, for a
 * variant edit, that product's variant list) — invalidating
 * `catalogKeys.product(productId)` plus `["catalog", "products"]` (the
 * list's key prefix, matching every `{ q }` filter variant without needing
 * to know which are mounted, same as `admin`'s query-key invalidation) and
 * `catalogKeys.variants(productId)` (the product-detail screen's separate
 * stock-joined variants query, `features/catalog/hooks.ts`
 * `useVariantsWithStock`) covers every screen that could be showing this
 * product right now.
 */
function useInvalidateProduct(productId: string) {
  const queryClient = useQueryClient();
  return () => {
    queryClient.invalidateQueries({ queryKey: catalogKeys.product(productId) });
    queryClient.invalidateQueries({ queryKey: catalogKeys.variants(productId) });
    queryClient.invalidateQueries({ queryKey: ["catalog", "products"] });
  };
}

/** `PATCH /products/{id}` (price, promo, active flag, low-stock threshold). */
export function useUpdateProduct(productId: string) {
  const invalidate = useInvalidateProduct(productId);
  return useMutation({
    mutationFn: (patch: ProductPatch) => updateProduct(productId, patch),
    onSuccess: invalidate,
  });
}

/** `PATCH /variants/{id}` (per-variant price override and active flag). */
export function useUpdateVariant(productId: string) {
  const invalidate = useInvalidateProduct(productId);
  return useMutation({
    mutationFn: ({ variantId, patch }: { variantId: string; patch: VariantPatch }) =>
      updateVariant(variantId, patch),
    onSuccess: invalidate,
  });
}

/** `POST /media` followed by `POST /products/{id}/images` — one photo,
 * uploaded then attached, as a single mutation so a caller only has one
 * pending/error state to show for "take/choose a photo". */
export function useUploadProductImage(productId: string) {
  const invalidate = useInvalidateProduct(productId);
  return useMutation({
    mutationFn: async (photo: {
      uri: string;
      mimeType: string;
      fileName: string;
      isCover: boolean;
    }) => {
      const media = await uploadMedia(photo.uri, photo.mimeType, photo.fileName);
      const create: ProductImageCreate = { mediaId: media.id, isCover: photo.isCover };
      return addProductImage(productId, create);
    },
    onSuccess: invalidate,
  });
}

/** `PATCH /products/{id}/images/{imageId}` — used here for "set as cover"
 * only (D-77 doesn't ask for reordering or variant-tagging on mobile). */
export function useUpdateProductImage(productId: string) {
  const invalidate = useInvalidateProduct(productId);
  return useMutation({
    mutationFn: ({ imageId, patch }: { imageId: string; patch: ProductImagePatch }) =>
      updateProductImage(productId, imageId, patch),
    onSuccess: invalidate,
  });
}

/** `DELETE /products/{id}/images/{imageId}`. */
export function useRemoveProductImage(productId: string) {
  const invalidate = useInvalidateProduct(productId);
  return useMutation({
    mutationFn: (imageId: string) => removeProductImage(productId, imageId),
    onSuccess: invalidate,
  });
}
