import type { Locale } from "@savdo/i18n";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { App, Button, Card, Form, type FormInstance, Skeleton } from "antd";
import type { ReactNode } from "react";
import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import {
  type ContentBlock,
  type ContentKey,
  type ContentResource,
  saveContent,
} from "../../../lib/content";
import { applyApiErrorToForm, notifyApiError } from "../../../lib/errors";

export interface ContentBlockCardProps<
  TValues extends object,
  TData extends Record<string, unknown>,
> {
  contentKey: ContentKey;
  title: string;
  locale: Locale;
  resource: ContentResource | undefined;
  isPending: boolean;
  /** Builds the form's initial values for `locale` from the resource. Each
   * card decides its own fallback (only the hours card falls back to `uz`
   * per D-104/D-107 — the task spec). */
  buildInitialValues: (resource: ContentResource | undefined, locale: Locale) => Partial<TValues>;
  /** Builds the `PUT .../{key}` payload from the form's current values,
   * stripping blank optional fields — an empty string sent for an optional
   * URL field is `422 VALIDATION_FAILED`, not "absent" (O-19), so it must
   * never be sent at all. */
  buildPayload: (values: TValues) => TData;
  children: (form: FormInstance<TValues>) => ReactNode;
  hint?: ReactNode;
}

/**
 * One "Landing content" card (T4 spec): loads a key's saved locales via the
 * shared `useContentResource` cache, shows a form for the active locale, and
 * saves that locale alone via `PUT /content/{key}`. Shared by all six blocks
 * (hero/about/hours/contacts/social/seo) — each supplies its own fields via
 * `children` and its own initial-values/payload mapping.
 */
export function ContentBlockCard<TValues extends object, TData extends Record<string, unknown>>({
  contentKey,
  title,
  locale,
  resource,
  isPending,
  buildInitialValues,
  buildPayload,
  children,
  hint,
}: ContentBlockCardProps<TValues, TData>) {
  const { t } = useTranslation();
  const { notification } = App.useApp();
  const queryClient = useQueryClient();
  const [form] = Form.useForm<TValues>();

  // Repopulates the form whenever the loaded resource or the active locale
  // changes — each locale tab mounts its own `ContentBlockCard` instance
  // (see `LandingContentPage`), so this also covers the initial populate
  // once loading finishes.
  useEffect(() => {
    if (!isPending) {
      form.setFieldsValue(buildInitialValues(resource, locale) as TValues);
    }
  }, [resource, locale, isPending, buildInitialValues, form.setFieldsValue]);

  const saveMutation = useMutation({
    mutationFn: (values: TValues) =>
      saveContent(contentKey, { locale, data: buildPayload(values) }),
    onSuccess: (saved: ContentBlock) => {
      queryClient.setQueryData<ContentResource>(["content", contentKey], (current) => {
        const base = current ?? { key: contentKey, locales: {} };
        return {
          ...base,
          locales: {
            ...base.locales,
            [locale]: { data: saved.data, updatedAt: saved.updatedAt, updatedBy: saved.updatedBy },
          },
        };
      });
      notification.success({ message: t("content.saved") });
    },
    onError: (error) => {
      if (!applyApiErrorToForm(form, error, t)) {
        notifyApiError(notification, error, t);
      }
    },
  });

  return (
    <Card title={title} size="small">
      {isPending ? (
        <Skeleton active />
      ) : (
        <Form<TValues>
          form={form}
          layout="vertical"
          onFinish={(values) => saveMutation.mutate(values)}
        >
          {hint}
          {children(form)}
          <Form.Item>
            <Button type="primary" htmlType="submit" loading={saveMutation.isPending}>
              {t("common.save")}
            </Button>
          </Form.Item>
        </Form>
      )}
    </Card>
  );
}
