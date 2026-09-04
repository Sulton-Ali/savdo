import { useMutation, useQueryClient } from "@tanstack/react-query";
import { App, Button, Card, Empty, Popconfirm, Select, Space, Tag, Upload } from "antd";
import type { RcFile } from "antd/es/upload";
import { ArrowDown, ArrowUp, Star, Trash2 } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import {
  type AttributeDefinition,
  addProductImage,
  type ProductImage,
  type ProductImagePatch,
  removeProductImage,
  reorderProductImages,
  updateProductImage,
  uploadMedia,
  type Variant,
} from "../../catalog/api";
import { MAX_PRODUCT_IMAGES, moveImage } from "../../catalog/images";
import { variantLabel } from "../../catalog/variants";
import { ApiError, notifyApiError } from "../../lib/errors";
import { ImageCropModal } from "./ImageCropModal";

const ACCEPTED_MIME = ["image/jpeg", "image/png", "image/webp"];
/** Client-side check only — the server enforces its own limit
 * (`contracts/openapi.yaml` `400 fields.file: too_long`). */
const MAX_UPLOAD_BYTES = 10 * 1024 * 1024;

/**
 * The product image gallery (T6b spec): a grid ordered by `sortOrder`, a
 * cover badge, per-image variant tagging, up/down reorder, remove, and an
 * upload button that opens a crop step before ever calling `POST /media`.
 *
 * The variant `Select` and the "set as cover" action both retag/re-cover an
 * image in place via `PATCH /products/{id}/images/{imageId}` (D-43) — one
 * atomic request each, not the earlier detach/reattach/reorder workaround.
 */
export function ImageGallery({
  productId,
  images,
  variants,
  attributeDefinitions,
  canWrite,
}: {
  productId: string;
  images: ProductImage[];
  variants: Variant[];
  attributeDefinitions: AttributeDefinition[];
  canWrite: boolean;
}) {
  const { t } = useTranslation();
  const { notification } = App.useApp();
  const queryClient = useQueryClient();
  const [pendingImageSrc, setPendingImageSrc] = useState<string | null>(null);
  // Every image currently mid-update (retag or cover change) — a `Set`,
  // not the single active mutation's `variables`, so two rapid updates on
  // different images (each its own `mutate` call against the same
  // `useMutation`) both stay locked rather than the second overwriting the
  // first's in-flight marker.
  const [updatingIds, setUpdatingIds] = useState<Set<string>>(new Set());

  const sorted = [...images].sort((a, b) => a.sortOrder - b.sortOrder);
  const atCap = sorted.length >= MAX_PRODUCT_IMAGES;

  function invalidate() {
    return queryClient.invalidateQueries({ queryKey: ["product", productId] });
  }

  function handleImageError(error: unknown) {
    if (error instanceof ApiError) {
      if (error.code === "RATE_LIMITED") {
        const message =
          error.retryAfterSeconds != null
            ? t("auth.errors.retryAfter", { seconds: error.retryAfterSeconds })
            : t("catalog.images.tooManyUploads");
        notification.error({ message });
        return;
      }
      const conflictField = (error.details as { field?: string }).field;
      if (error.code === "CONFLICT" && conflictField === "mediaId") {
        notification.error({ message: t("catalog.images.alreadyAttached") });
        return;
      }
      if (error.code === "VALIDATION_FAILED") {
        // The merged contract's cap error is `fields.mediaId: invalid`
        // only (`contracts/openapi.yaml` `addProductImage`) — no
        // `fields.images` variant exists.
        const fields = (error.details as { fields?: Record<string, string> }).fields ?? {};
        if (fields.mediaId === "invalid") {
          notification.error({ message: t("catalog.images.capReached") });
          return;
        }
      }
    }
    notifyApiError(notification, error, t);
  }

  const uploadMutation = useMutation({
    mutationFn: async (blob: Blob) => {
      const media = await uploadMedia(blob);
      return addProductImage(productId, { mediaId: media.id, isCover: sorted.length === 0 });
    },
    onSuccess: async () => {
      await invalidate();
      notification.success({ message: t("catalog.images.uploaded") });
    },
    onError: handleImageError,
  });

  const reorderMutation = useMutation({
    mutationFn: (body: { imageIds: string[]; coverImageId?: string }) =>
      reorderProductImages(productId, body),
    onSuccess: () => invalidate(),
    onError: (error) => notifyApiError(notification, error, t),
  });

  const removeMutation = useMutation({
    mutationFn: (imageId: string) => removeProductImage(productId, imageId),
    onSuccess: () => invalidate(),
    onError: (error) => notifyApiError(notification, error, t),
  });

  // Backs both the variant `Select` (retag) and the "set as cover" action:
  // one `PATCH /products/{id}/images/{imageId}` (D-43) per call, so the two
  // never race each other into inconsistent remove/add/reorder steps the
  // old workaround needed.
  const updateImageMutation = useMutation({
    mutationFn: ({ imageId, patch }: { imageId: string; patch: ProductImagePatch }) =>
      updateProductImage(productId, imageId, patch),
    onMutate: ({ imageId }) => {
      setUpdatingIds((current) => new Set(current).add(imageId));
    },
    onSuccess: () => invalidate(),
    onError: (error) => notifyApiError(notification, error, t),
    onSettled: (_data, _error, { imageId }) => {
      setUpdatingIds((current) => {
        const next = new Set(current);
        next.delete(imageId);
        return next;
      });
    },
  });

  function beforeUpload(file: RcFile): boolean | string {
    if (!ACCEPTED_MIME.includes(file.type)) {
      notification.error({ message: t("catalog.images.invalidType") });
      return Upload.LIST_IGNORE;
    }
    if (file.size > MAX_UPLOAD_BYTES) {
      notification.error({ message: t("catalog.images.tooLarge") });
      return Upload.LIST_IGNORE;
    }
    setPendingImageSrc(URL.createObjectURL(file));
    // Never let rc-upload send the raw file — the crop modal produces the
    // Blob that actually gets uploaded, via `uploadMutation` below.
    return false;
  }

  function closeCropModal() {
    if (pendingImageSrc) {
      URL.revokeObjectURL(pendingImageSrc);
    }
    setPendingImageSrc(null);
  }

  function handleReorder(imageId: string, direction: "up" | "down") {
    const next = moveImage(sorted, imageId, direction);
    if (next === sorted) {
      return;
    }
    reorderMutation.mutate({ imageIds: next.map((image) => image.id) });
  }

  function handleSetCover(imageId: string) {
    updateImageMutation.mutate({ imageId, patch: { isCover: true } });
  }

  const variantOptions = variants.map((variant) => ({
    value: variant.id,
    label: variantLabel(variant, attributeDefinitions),
  }));
  const variantById = new Map(variants.map((variant) => [variant.id, variant]));

  /** The variant's label when the image is tagged to one, else the gallery
   * title — always more meaningful than an empty `alt` for a product photo. */
  function altTextFor(image: ProductImage): string {
    const variant = image.variantId ? variantById.get(image.variantId) : undefined;
    return variant ? variantLabel(variant, attributeDefinitions) : t("catalog.images.title");
  }

  return (
    <Card title={t("catalog.images.title")} size="small">
      <Space direction="vertical" style={{ width: "100%" }}>
        {canWrite && (
          <Space direction="vertical" size={4}>
            <Upload
              accept={ACCEPTED_MIME.join(",")}
              showUploadList={false}
              disabled={atCap || uploadMutation.isPending}
              beforeUpload={beforeUpload}
            >
              <Button disabled={atCap} loading={uploadMutation.isPending}>
                {t("catalog.images.upload")}
              </Button>
            </Upload>
            <span>{atCap ? t("catalog.images.capReached") : t("catalog.images.uploadHint")}</span>
          </Space>
        )}

        {sorted.length === 0 ? (
          <Empty description={false} />
        ) : (
          <div style={{ display: "flex", flexWrap: "wrap", gap: 12 }}>
            {sorted.map((image, index) => {
              const isUpdating = updatingIds.has(image.id);
              return (
                <Card
                  key={image.id}
                  size="small"
                  style={{ width: 180 }}
                  cover={
                    <img
                      src={image.urls.card}
                      alt={altTextFor(image)}
                      style={{ objectFit: "cover", height: 140, width: "100%" }}
                    />
                  }
                >
                  {image.isCover && <Tag color="gold">{t("catalog.images.cover")}</Tag>}
                  {canWrite && (
                    <Space direction="vertical" style={{ width: "100%", marginTop: 8 }}>
                      <Select
                        size="small"
                        style={{ width: "100%" }}
                        allowClear
                        disabled={isUpdating}
                        loading={isUpdating}
                        placeholder={t("catalog.images.noVariant")}
                        aria-label={t("catalog.images.variantTag")}
                        value={image.variantId ?? undefined}
                        options={variantOptions}
                        onChange={(value: string | undefined) =>
                          updateImageMutation.mutate({
                            imageId: image.id,
                            patch: { variantId: value ?? null },
                          })
                        }
                      />
                      <Space size={4}>
                        <Button
                          size="small"
                          aria-label={t("catalog.images.moveUp")}
                          disabled={isUpdating || index === 0}
                          onClick={() => handleReorder(image.id, "up")}
                          icon={<ArrowUp size={14} />}
                        />
                        <Button
                          size="small"
                          aria-label={t("catalog.images.moveDown")}
                          disabled={isUpdating || index === sorted.length - 1}
                          onClick={() => handleReorder(image.id, "down")}
                          icon={<ArrowDown size={14} />}
                        />
                        {!image.isCover && (
                          <Button
                            size="small"
                            aria-label={t("catalog.images.setCover")}
                            disabled={isUpdating}
                            onClick={() => handleSetCover(image.id)}
                            icon={<Star size={14} />}
                          />
                        )}
                        <Popconfirm
                          title={t("catalog.images.confirmRemove")}
                          onConfirm={() => removeMutation.mutate(image.id)}
                        >
                          <Button
                            size="small"
                            danger
                            disabled={isUpdating}
                            aria-label={t("catalog.images.remove")}
                            icon={<Trash2 size={14} />}
                          />
                        </Popconfirm>
                      </Space>
                    </Space>
                  )}
                </Card>
              );
            })}
          </div>
        )}
      </Space>

      {pendingImageSrc && (
        <ImageCropModal
          imageSrc={pendingImageSrc}
          onCancel={closeCropModal}
          onCropped={(blob) => {
            closeCropModal();
            uploadMutation.mutate(blob);
          }}
        />
      )}
    </Card>
  );
}
