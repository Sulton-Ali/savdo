import type { components } from "@savdo/api-client";

import { api } from "@/lib/api";

export type ProductPatch = components["schemas"]["ProductPatch"];
export type VariantPatch = components["schemas"]["VariantPatch"];
export type MediaFile = components["schemas"]["MediaFile"];
export type ProductImage = components["schemas"]["ProductImage"];
export type ProductImageCreate = components["schemas"]["ProductImageCreate"];
export type ProductImagePatch = components["schemas"]["ProductImagePatch"];
export type ErrorCode = components["schemas"]["ErrorCode"];
export type ApiErrorBody = components["schemas"]["Error"];

/**
 * Thrown by every function in this module on a non-2xx response. Carries
 * the machine-readable `code` and `details` (ADR-013) — callers translate
 * to a display sentence via `@savdo/i18n`, never show a raw API message.
 * Mirrors `admin/src/lib/errors.ts`'s `ApiError` (this module's own copy:
 * `../api.ts`'s `CatalogApiError` carries only `code`, not `details`/
 * `retryAfterSeconds`, which the image cap (`VALIDATION_FAILED`
 * `fields.mediaId`) and the media upload queue (`RATE_LIMITED`) both need).
 */
export class CatalogEditApiError extends Error {
  readonly code: ErrorCode;
  readonly details: Record<string, unknown>;
  /** Seconds to wait before retrying, from the `Retry-After` response
   * header on a `429 RATE_LIMITED` (`POST /media`'s admission queue).
   * `undefined` for every other error, or when the header is absent. */
  readonly retryAfterSeconds?: number;

  constructor(body: ApiErrorBody, retryAfterSeconds?: number) {
    super(`catalog edit request failed: ${body.error.code}`);
    this.name = "CatalogEditApiError";
    this.code = body.error.code;
    this.details = (body.error.details ?? {}) as Record<string, unknown>;
    this.retryAfterSeconds = retryAfterSeconds;
  }
}

function parseRetryAfterSeconds(response: Response): number | undefined {
  const header = response.headers.get("Retry-After");
  if (!header) {
    return undefined;
  }
  const seconds = Number(header);
  return Number.isFinite(seconds) ? seconds : undefined;
}

/** `PATCH /products/{id}` — requires `catalog.write` (manager+). Partial
 * update; `null` clears a nullable field (D-35). */
export async function updateProduct(
  id: string,
  body: ProductPatch,
): Promise<components["schemas"]["Product"]> {
  const { data, error } = await api.PATCH("/products/{id}", {
    params: { path: { id } },
    body,
  });
  if (error) {
    throw new CatalogEditApiError(error);
  }
  return data;
}

/** `PATCH /variants/{id}` — requires `catalog.write`. Partial update; `null`
 * clears `priceOverride` (D-35). */
export async function updateVariant(
  id: string,
  body: VariantPatch,
): Promise<components["schemas"]["Variant"]> {
  const { data, error } = await api.PATCH("/variants/{id}", {
    params: { path: { id } },
    body,
  });
  if (error) {
    throw new CatalogEditApiError(error);
  }
  return data;
}

/**
 * `POST /media` — requires `catalog.write` (multipart). Builds real
 * `multipart/form-data` from React Native's own file-object convention
 * (`{ uri, name, type }`, not a web `File`/`Blob` — there is no such thing
 * on-device): RN's `FormData` polyfill (`Libraries/Network/FormData.js`)
 * special-cases exactly this shape and forwards it to the native
 * `XMLHttpRequest` module, and `lib/api.ts`'s per-request wrapper passes a
 * `FormData` body through untouched (never `.text()`s it), so this is the
 * only place besides `admin/src/catalog/api.ts`'s web `File`/`Blob`
 * version that needs a `bodySerializer` override at all. The generated
 * `MediaUpload` schema types `file` as `string` (openapi-typescript's
 * rendering of `format: binary`); the wire body is real multipart, so this
 * casts around that mismatch rather than widening the contract by hand
 * (ADR-002).
 */
export async function uploadMedia(
  uri: string,
  mimeType: string,
  fileName: string,
): Promise<MediaFile> {
  const { data, error, response } = await api.POST("/media", {
    body: { file: uri } as unknown as { file: string },
    bodySerializer() {
      const formData = new FormData();
      formData.append("file", { uri, name: fileName, type: mimeType } as unknown as Blob);
      return formData;
    },
  });
  if (error) {
    // The admission queue returns 429 RATE_LIMITED with Retry-After when
    // full (`api/internal/media/service.go`); surface it to the caller.
    throw new CatalogEditApiError(error, parseRetryAfterSeconds(response));
  }
  return data;
}

/** `POST /products/{id}/images` — requires `catalog.write`. `400
 * fields.mediaId: invalid` once the product already has 8 images (D-34);
 * `409 CONFLICT details.field: mediaId` when this media file is already
 * attached to the product. */
export async function addProductImage(
  productId: string,
  body: ProductImageCreate,
): Promise<ProductImage> {
  const { data, error } = await api.POST("/products/{id}/images", {
    params: { path: { id: productId } },
    body,
  });
  if (error) {
    throw new CatalogEditApiError(error);
  }
  return data;
}

/** `PATCH /products/{id}/images/{imageId}` — requires `catalog.write`
 * (D-43). Partial update: `isCover` omitted leaves it unchanged. */
export async function updateProductImage(
  productId: string,
  imageId: string,
  body: ProductImagePatch,
): Promise<ProductImage> {
  const { data, error } = await api.PATCH("/products/{id}/images/{imageId}", {
    params: { path: { id: productId, imageId } },
    body,
  });
  if (error) {
    throw new CatalogEditApiError(error);
  }
  return data;
}

/** `DELETE /products/{id}/images/{imageId}` — requires `catalog.write`. */
export async function removeProductImage(productId: string, imageId: string): Promise<void> {
  const { error } = await api.DELETE("/products/{id}/images/{imageId}", {
    params: { path: { id: productId, imageId } },
  });
  if (error) {
    throw new CatalogEditApiError(error);
  }
}
