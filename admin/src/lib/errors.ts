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
  /** Seconds to wait before retrying, from the `Retry-After` response
   * header on a `429 RATE_LIMITED` (mirrors `auth/api.ts`'s `ApiAuthError`).
   * `undefined` for every other error, or when the header is absent. */
  readonly retryAfterSeconds?: number;

  constructor(body: ApiErrorBody, retryAfterSeconds?: number) {
    super(`api request failed: ${body.error.code}`);
    this.name = "ApiError";
    this.code = body.error.code;
    this.details = (body.error.details ?? {}) as Record<string, unknown>;
    this.retryAfterSeconds = retryAfterSeconds;
  }
}

/** Reads the `Retry-After` header (seconds) from a raw `Response`, e.g. for
 * a `429 RATE_LIMITED` (`docs/05-API.md` § Conventions). Mirrors
 * `auth/api.ts`'s identical helper — kept separate because that module has
 * its own `ApiAuthError`, not this shared `ApiError`. */
export function parseRetryAfterSeconds(response: Response): number | undefined {
  const header = response.headers.get("Retry-After");
  if (!header) {
    return undefined;
  }
  const seconds = Number(header);
  return Number.isFinite(seconds) ? seconds : undefined;
}

/**
 * Parses a server field name into an Ant Design `NamePath` array. Most
 * fields are a single flat name (`"slug"` → `["slug"]`); a nested array
 * field — `content.Service`'s `"days[2].close"` for `ContentHours.days`
 * (`api/internal/content/validate.go`) — becomes `["days", 2, "close"]`,
 * matching the `Form.Item name={["days", index, "close"]}` path `HoursCard`
 * actually renders. AntD 6's `form.setFields`/`getFieldInstance` only match
 * a `NamePath` array, never the bracket string itself.
 */
function parseFieldNamePath(name: string): (string | number)[] {
  const path: (string | number)[] = [];
  const segment = /([^[\].]+)|\[(\d+)\]/g;
  let match: RegExpExecArray | null = segment.exec(name);
  while (match !== null) {
    path.push(match[2] !== undefined ? Number(match[2]) : (match[1] as string));
    match = segment.exec(name);
  }
  return path.length > 0 ? path : [name];
}

/**
 * Maps a `VALIDATION_FAILED` (`details.fields`) or `CONFLICT` (`details.field`)
 * error onto Ant Design form fields via `form.setFields` (D-26). Returns
 * `true` when it applied at least one field error, so the caller knows not to
 * also show a page-level notification for the same failure.
 *
 * Only names that resolve to a currently-mounted `Form.Item` (via
 * `form.getFieldInstance`, after `parseFieldNamePath` turns a bracketed
 * server name into a real `NamePath`) are used. A server field name that
 * doesn't match any rendered field — e.g. a flat `"name"` from a validation
 * error on a translation entry, which every form here renders as a nested
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
    const entries = Object.entries(fields ?? {})
      .map(([name, reason]) => ({ namePath: parseFieldNamePath(name), reason }))
      .filter(({ namePath }) => form.getFieldInstance(namePath) != null);
    if (entries.length === 0) {
      return false;
    }
    form.setFields(
      entries.map(({ namePath, reason }) => ({
        name: namePath,
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
    if (!field) {
      return false;
    }
    const namePath = parseFieldNamePath(field);
    if (form.getFieldInstance(namePath) == null) {
      return false;
    }
    form.setFields([{ name: namePath, errors: [t("errors.field.conflict")] }]);
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
