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
  removeProductImage,
  reorderProductImages,
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
 * There is no endpoint to change a single already-attached image's
 * `variantId` in place (`docs/05-API.md` § Catalogue and media only lists
 * create/delete/reorder) — the variant `Select` below works around that by
 * detaching and reattaching the same media file, then restoring its exact
 * position and cover flag via `PATCH .../images/order`.
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

  const sorted = [...images].sort((a, b) => a.sortOrder - b.sortOrder);
  const atCap = sorted.length >= MAX_PRODUCT_IMAGES;

  function invalidate() {
    return queryClient.invalidateQueries({ queryKey: ["product", productId] });
  }

  function handleImageError(error: unknown) {
    if (error instanceof ApiError) {
      const conflictField = (error.details as { field?: string }).field;
      if (error.code === "CONFLICT" && conflictField === "mediaId") {
        notification.error({ message: t("catalog.images.alreadyAttached") });
        return;
      }
      if (error.code === "VALIDATION_FAILED") {
        const fields = (error.details as { fields?: Record<string, string> }).fields ?? {};
        if (fields.mediaId === "invalid" || fields.images === "too_long") {
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

  const retagMutation = useMutation({
    mutationFn: async ({ image, variantId }: { image: ProductImage; variantId: string | null }) => {
      await removeProductImage(productId, image.id);
      const created = await addProductImage(productId, {
        mediaId: image.mediaId,
        ...(variantId ? { variantId } : {}),
        isCover: image.isCover,
      });
      const orderedIds = sorted.map((img) => (img.id === image.id ? created.id : img.id));
      await reorderProductImages(productId, {
        imageIds: orderedIds,
        ...(image.isCover ? { coverImageId: created.id } : {}),
      });
    },
    onSuccess: () => invalidate(),
    onError: handleImageError,
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
    reorderMutation.mutate({
      imageIds: sorted.map((image) => image.id),
      coverImageId: imageId,
    });
  }

  const variantOptions = variants.map((variant) => ({
    value: variant.id,
    label: variantLabel(variant, attributeDefinitions),
  }));

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
            {sorted.map((image, index) => (
              <Card
                key={image.id}
                size="small"
                style={{ width: 180 }}
                cover={
                  <img
                    src={image.urls.card}
                    alt=""
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
                      placeholder={t("catalog.images.noVariant")}
                      aria-label={t("catalog.images.variantTag")}
                      value={image.variantId ?? undefined}
                      options={variantOptions}
                      onChange={(value: string | undefined) =>
                        retagMutation.mutate({ image, variantId: value ?? null })
                      }
                    />
                    <Space size={4}>
                      <Button
                        size="small"
                        aria-label={t("catalog.images.moveUp")}
                        disabled={index === 0}
                        onClick={() => handleReorder(image.id, "up")}
                        icon={<ArrowUp size={14} />}
                      />
                      <Button
                        size="small"
                        aria-label={t("catalog.images.moveDown")}
                        disabled={index === sorted.length - 1}
                        onClick={() => handleReorder(image.id, "down")}
                        icon={<ArrowDown size={14} />}
                      />
                      {!image.isCover && (
                        <Button
                          size="small"
                          aria-label={t("catalog.images.setCover")}
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
                          aria-label={t("catalog.images.remove")}
                          icon={<Trash2 size={14} />}
                        />
                      </Popconfirm>
                    </Space>
                  </Space>
                )}
              </Card>
            ))}
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
