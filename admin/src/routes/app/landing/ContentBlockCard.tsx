import type { Locale } from "@savdo/i18n";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { App, Button, Card, Form, type FormInstance, Skeleton } from "antd";
import type { ReactNode } from "react";
import { useEffect, useRef } from "react";
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

  // All three locale tabs mount their own `ContentBlockCard` instance (see
  // `LandingContentPage`) but share one `["content", key]` query cache
  // (`lib/content.ts`) — so this instance's `resource` prop can change for
  // reasons that have nothing to do with its own locale: a background
  // refetch, or *another* locale's save landing in the same cache entry.
  // Populate unconditionally only on this instance's first successful load;
  // after that, resync only when the user has not made an edit since, so an
  // in-progress edit in one locale is never silently overwritten by
  // something happening in another (review MAJOR 2). `locale` itself never
  // changes across one instance's lifetime, so `hasLoadedRef` only needs to
  // track "have we populated at all yet".
  //
  // "Has an edit" is tracked with `userEditedRef`, set from the `Form`'s
  // `onValuesChange` below — fired only by a real field interaction, never
  // by our own `form.setFieldsValue` calls (`@rc-component/form`'s
  // `setFieldsValue` never calls the `onValuesChange` callback, only a
  // field's own trigger does) — unlike `form.isFieldsTouched()`, which a
  // first review round found *does* flip true from `setFieldsValue` itself
  // whenever it sets a field away from its unset default, so it can never
  // go false again and a legitimate untouched resync (e.g. a background
  // refetch) never lands (review MAJOR, re-review).
  const hasLoadedRef = useRef(false);
  const userEditedRef = useRef(false);
  useEffect(() => {
    if (isPending) {
      return;
    }
    if (!hasLoadedRef.current || !userEditedRef.current) {
      hasLoadedRef.current = true;
      userEditedRef.current = false;
      form.setFieldsValue(buildInitialValues(resource, locale) as TValues);
    }
  }, [resource, locale, isPending, buildInitialValues, form]);

  const saveMutation = useMutation({
    mutationFn: (values: TValues) =>
      saveContent(contentKey, { locale, data: buildPayload(values) }),
    onSuccess: (saved: ContentBlock) => {
      const updated = queryClient.setQueryData<ContentResource>(
        ["content", contentKey],
        (current) => {
          const base = current ?? { key: contentKey, locales: {} };
          return {
            ...base,
            locales: {
              ...base.locales,
              [locale]: {
                data: saved.data,
                updatedAt: saved.updatedAt,
                updatedBy: saved.updatedBy,
              },
            },
          };
        },
      );
      // This locale's own successful save: always resync to the
      // server-confirmed values, even though this form has an edit pending
      // from the submit that just happened — the guard above only protects
      // against *other* tabs' pushes, never this tab's own save.
      if (updated) {
        userEditedRef.current = false;
        form.setFieldsValue(buildInitialValues(updated, locale) as TValues);
      }
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
          onValuesChange={() => {
            userEditedRef.current = true;
          }}
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
