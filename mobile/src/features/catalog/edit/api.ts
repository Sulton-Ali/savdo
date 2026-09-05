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
 * Reads a local `file://`/`content://` URI (from `expo-image-picker`) into a
 * real `Blob`, via `XMLHttpRequest`'s `responseType: "blob"` — the
 * long-standing React Native technique for turning a picked asset into a
 * `Blob` without a native-file-reading dependency of its own. Deliberately
 * *not* `fetch(uri)`: Expo SDK 57 installs its own WinterCG `fetch` as the
 * global (`expo/src/winter/runtime.native.ts`'s `install('fetch', ...)`,
 * confirmed against the installed package, not training data), which talks
 * to a native HTTP client and does not understand a local file URI;
 * `XMLHttpRequest` is a separate global that SDK 57 leaves untouched, so it
 * still reads local files exactly as RN always has.
 */
function readAsBlob(uri: string): Promise<Blob> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.responseType = "blob";
    xhr.onload = () => resolve(xhr.response as Blob);
    xhr.onerror = () => reject(new Error(`readAsBlob: failed to read ${uri}`));
    xhr.open("GET", uri, true);
    xhr.send();
  });
}

/**
 * `POST /media` — requires `catalog.write` (multipart). Builds real
 * `multipart/form-data` from a `Blob` (via `readAsBlob`), not RN's classic
 * `{ uri, name, type }` FormData file-object convention: that shape is
 * accepted by `FormData.append` under Expo SDK 57's own global `fetch`
 * (`expo/src/winter/FormData.ts`'s patched `append` even documents the
 * overload), but that `fetch`'s multipart body builder
 * (`expo/src/winter/fetch/convertFormData.ts`) only actually serializes a
 * part that is a real `Blob` or has a `.bytes()` method — a plain
 * `{uri, name, type}` part throws `Unsupported FormDataPart implementation`
 * at request time (reproduced on-device: the 8.x KB test upload never even
 * reached the API's access log). The generated `MediaUpload` schema types
 * `file` as `string` (openapi-typescript's rendering of `format: binary`);
 * the wire body is real multipart, so this casts around that mismatch
 * rather than widening the contract by hand (ADR-002).
 */
export async function uploadMedia(
  uri: string,
  mimeType: string,
  fileName: string,
): Promise<MediaFile> {
  const rawBlob = await readAsBlob(uri);
  // `XMLHttpRequest`'s blob response type isn't always the picker's own
  // `mimeType` (e.g. an extensionless cache path can come back
  // `application/octet-stream`); rewrap only when it actually differs, so
  // the multipart part's `Content-Type` always matches what the caller
  // asked to upload.
  const blob = rawBlob.type === mimeType ? rawBlob : new Blob([rawBlob], { type: mimeType });
  const { data, error, response } = await api.POST("/media", {
    body: { file: blob } as unknown as { file: string },
    bodySerializer() {
      const formData = new FormData();
      formData.append("file", blob, fileName);
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
