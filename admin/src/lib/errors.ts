import type { components } from "@savdo/api-client";
import type { FormInstance } from "antd";
import type { NotificationInstance } from "antd/es/notification/interface";
import type { TFunction } from "i18next";

export type ErrorCode = components["schemas"]["ErrorCode"];
export type ApiErrorBody = components["schemas"]["Error"];

/**
 * Details shape for `VALIDATION_FAILED` (`{ fields: { "<field>": "<reason>" } }`)
 * and `CONFLICT` (`{ field: "<field>" }`) per `docs/05-API.md` § Conventions.
 * The generated `Error.details` is `Record<string, never>` (openapi-typescript's
 * shape for an untyped `object` schema — ADR-013 keeps it deliberately generic
 * per code), so this module narrows it with a cast rather than widening the
 * contract by hand (ADR-002).
 */
interface ValidationFailedDetails {
  fields?: Record<string, string>;
}

interface ConflictDetails {
  field?: string;
}

/**
 * Thrown by every domain `api.ts` module (staff, locations, settings, ...) on
 * a non-2xx response. Carries only the machine-readable `code`/`details`
 * (ADR-013) — callers translate to a display sentence via `@savdo/i18n`, never
 * show a raw API message.
 */
export class ApiError extends Error {
  readonly code: ErrorCode;
  readonly details: Record<string, unknown>;

  constructor(body: ApiErrorBody) {
    super(`api request failed: ${body.error.code}`);
    this.name = "ApiError";
    this.code = body.error.code;
    this.details = (body.error.details ?? {}) as Record<string, unknown>;
  }
}

/**
 * Maps a `VALIDATION_FAILED` (`details.fields`) or `CONFLICT` (`details.field`)
 * error onto Ant Design form fields via `form.setFields` (D-26). Returns
 * `true` when it applied at least one field error, so the caller knows not to
 * also show a page-level notification for the same failure.
 *
 * Only names that resolve to a currently-mounted `Form.Item` (via
 * `form.getFieldInstance`) are used. A server field name that doesn't match
 * any rendered field — e.g. a flat `"name"` from a validation error on a
 * translation entry, which every form here renders as a nested
 * `["translations", locale, "name"]` path with no locale the server can
 * know — must fall back to a page-level notification instead of being
 * silently swallowed by `form.setFields` on a field nothing displays.
 */
export function applyApiErrorToForm(form: FormInstance, error: unknown, t: TFunction): boolean {
  if (!(error instanceof ApiError)) {
    return false;
  }

  if (error.code === "VALIDATION_FAILED") {
    const { fields } = error.details as ValidationFailedDetails;
    const entries = Object.entries(fields ?? {}).filter(
      ([name]) => form.getFieldInstance(name) != null,
    );
    if (entries.length === 0) {
      return false;
    }
    form.setFields(
      entries.map(([name, reason]) => ({
        name,
        errors: [
          t(`errors.field.${reason}`, {
            defaultValue: t("errors.field.invalid"),
          }),
        ],
      })),
    );
    return true;
  }

  if (error.code === "CONFLICT") {
    const { field } = error.details as ConflictDetails;
    if (!field || form.getFieldInstance(field) == null) {
      return false;
    }
    form.setFields([{ name: field, errors: [t("errors.field.conflict")] }]);
    return true;
  }

  return false;
}

/** Translation key for a page-level notification, for any error `applyApiErrorToForm`
 * did not already turn into a field error (403, 404, or anything else). */
function pageErrorKey(error: unknown): "errors.forbidden" | "errors.notFound" | "errors.generic" {
  if (error instanceof ApiError) {
    if (error.code === "FORBIDDEN") {
      return "errors.forbidden";
    }
    if (error.code === "NOT_FOUND") {
      return "errors.notFound";
    }
  }
  return "errors.generic";
}

/** Shows a page-level notification for an error `applyApiErrorToForm` did not
 * handle (403, 404, or anything else) via `App.useApp().notification`. */
export function notifyApiError(
  notification: NotificationInstance,
  error: unknown,
  t: TFunction,
): void {
  notification.error({ title: t(pageErrorKey(error)) });
}
