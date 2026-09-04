import type { components } from "@savdo/api-client";
import { type Locale, locales } from "@savdo/i18n";
import type { FormInstance } from "antd";

export type Translations = components["schemas"]["Translations"];

export interface TranslationFieldValues {
  name?: string;
  description?: string;
}

/** Shape of the `translations` group of fields in a create/edit `Form`,
 * e.g. `form.getFieldValue("translations")`. */
export type TranslationsFormValue = Partial<Record<Locale, TranslationFieldValues>>;

function trimmedEntry(
  entry: TranslationFieldValues | undefined,
): { name: string; description?: string } | null {
  const name = entry?.name?.trim();
  if (!name) {
    return null;
  }
  const description = entry?.description?.trim();
  return description ? { name, description } : { name };
}

/**
 * Builds a `Translations` payload for a create request: every locale whose
 * name field is non-empty. The server enforces that the shop's own default
 * locale is included (`contracts/openapi.yaml` `Translations` description) —
 * the form should mark that locale's name field required so this never
 * comes back empty for it.
 */
export function buildTranslationsForCreate(
  values: TranslationsFormValue | undefined,
): Translations {
  const result: Translations = {};
  for (const locale of locales) {
    const entry = trimmedEntry(values?.[locale]);
    if (entry) {
      result[locale] = entry;
    }
  }
  return result;
}

/**
 * Builds a `Translations` payload for a PATCH request: only locales the user
 * actually touched (`form.isFieldTouched`), each sent as a full replacement
 * of that locale's stored entry (D-35 — untouched locales stay absent so
 * they are left unchanged). Returns `undefined` when nothing was touched, so
 * callers can omit `translations` from the patch body entirely.
 */
export function buildTranslationsForPatch(
  form: FormInstance,
  values: TranslationsFormValue | undefined,
): Translations | undefined {
  let result: Translations | undefined;
  for (const locale of locales) {
    const touched =
      form.isFieldTouched(["translations", locale, "name"]) ||
      form.isFieldTouched(["translations", locale, "description"]);
    if (!touched) {
      continue;
    }
    const entry = trimmedEntry(values?.[locale]);
    if (!entry) {
      continue;
    }
    result ??= {};
    result[locale] = entry;
  }
  return result;
}
