import type { Locale } from "@savdo/i18n";
import { useMutation } from "@tanstack/react-query";
import { App, Button, Form, Input, Space, Tag, Upload } from "antd";
import type { RcFile } from "antd/es/upload";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { type MediaFile, uploadMedia } from "../../../catalog/api";
import type { ContentHero, ContentResource } from "../../../lib/content";
import { notifyApiError } from "../../../lib/errors";
import { ContentBlockCard } from "./ContentBlockCard";

const ACCEPTED_MIME = ["image/jpeg", "image/png", "image/webp"];
/** Client-side check only — the server enforces its own limit, same as the
 * product image gallery (`ImageGallery.tsx`). */
const MAX_UPLOAD_BYTES = 10 * 1024 * 1024;

interface HeroFormValues {
  title?: string;
  tagline?: string;
  imageMediaId?: string;
}

function heroInitialValues(
  resource: ContentResource | undefined,
  locale: Locale,
): Partial<HeroFormValues> {
  const data = resource?.locales[locale]?.data as ContentHero | undefined;
  return { title: data?.title, tagline: data?.tagline, imageMediaId: data?.imageMediaId };
}

function buildHeroPayload(values: HeroFormValues): ContentHero {
  const payload: ContentHero = { title: values.title?.trim() ?? "" };
  const tagline = values.tagline?.trim();
  if (tagline) {
    payload.tagline = tagline;
  }
  if (values.imageMediaId) {
    payload.imageMediaId = values.imageMediaId;
  }
  return payload;
}

/**
 * The hero image field: uploads through the same `POST /media` flow as
 * product images (`ImageGallery.tsx`), then stores only `imageMediaId` on
 * the form (O-19) — a `Form.Item name="imageMediaId"` wraps this and injects
 * `value`/`onChange`.
 *
 * `GET /content/{key}` returns only the raw `imageMediaId`, not a resolved
 * URL (there is no `GET /media/{id}` in the contract), so an already-saved
 * image cannot be previewed after a reload — only right after this session
 * uploads one. Until reload, the just-uploaded file's own response (which
 * does carry `urls`) is kept in local state for the preview; afterwards a
 * plain "image saved" tag stands in for it.
 */
function HeroImageField({
  value,
  onChange,
}: {
  value?: string;
  onChange?: (id: string | undefined) => void;
}) {
  const { t } = useTranslation();
  const { notification } = App.useApp();
  const [preview, setPreview] = useState<MediaFile | null>(null);

  const uploadMutation = useMutation({
    mutationFn: (file: Blob) => uploadMedia(file),
    onSuccess: (media) => {
      setPreview(media);
      onChange?.(media.id);
    },
    onError: (error) => notifyApiError(notification, error, t),
  });

  function beforeUpload(file: RcFile): boolean {
    if (!ACCEPTED_MIME.includes(file.type)) {
      notification.error({ message: t("catalog.images.invalidType") });
      return false;
    }
    if (file.size > MAX_UPLOAD_BYTES) {
      notification.error({ message: t("catalog.images.tooLarge") });
      return false;
    }
    uploadMutation.mutate(file);
    return false;
  }

  function handleRemove() {
    setPreview(null);
    onChange?.(undefined);
  }

  return (
    <Space direction="vertical">
      {preview ? (
        <img
          src={preview.urls.card}
          alt={t("content.hero.image")}
          style={{ width: 240, borderRadius: 8 }}
        />
      ) : (
        value && <Tag>{t("content.hero.imageSet")}</Tag>
      )}
      <Space>
        <Upload
          accept={ACCEPTED_MIME.join(",")}
          showUploadList={false}
          disabled={uploadMutation.isPending}
          beforeUpload={beforeUpload}
        >
          <Button loading={uploadMutation.isPending}>
            {value ? t("content.hero.replaceImage") : t("content.hero.uploadImage")}
          </Button>
        </Upload>
        {value && (
          <Button danger onClick={handleRemove}>
            {t("content.hero.removeImage")}
          </Button>
        )}
      </Space>
    </Space>
  );
}

export function HeroCard({
  locale,
  resource,
  isPending,
}: {
  locale: Locale;
  resource: ContentResource | undefined;
  isPending: boolean;
}) {
  const { t } = useTranslation();
  return (
    <ContentBlockCard<HeroFormValues, ContentHero>
      contentKey="hero"
      title={t("content.keys.hero")}
      locale={locale}
      resource={resource}
      isPending={isPending}
      buildInitialValues={heroInitialValues}
      buildPayload={buildHeroPayload}
    >
      {() => (
        <>
          <Form.Item name="title" label={t("content.hero.title")} rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="tagline" label={t("content.hero.tagline")}>
            <Input />
          </Form.Item>
          <Form.Item name="imageMediaId" label={t("content.hero.image")}>
            <HeroImageField />
          </Form.Item>
        </>
      )}
    </ContentBlockCard>
  );
}
