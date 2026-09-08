import type { components } from "@savdo/api-client";
import { useQuery } from "@tanstack/react-query";

import { api } from "./api";
import { ApiError } from "./errors";

export type ContentKey = components["schemas"]["ContentKey"];
export type ContentResource = components["schemas"]["ContentResource"];
export type ContentLocaleBlock = components["schemas"]["ContentLocaleBlock"];
export type ContentPut = components["schemas"]["ContentPut"];
export type ContentBlock = components["schemas"]["ContentBlock"];
export type ContentHero = components["schemas"]["ContentHero"];
export type ContentAbout = components["schemas"]["ContentAbout"];
export type ContentHours = components["schemas"]["ContentHours"];
export type ContentHoursDay = components["schemas"]["ContentHoursDay"];
export type ContentContacts = components["schemas"]["ContentContacts"];
export type ContentSocial = components["schemas"]["ContentSocial"];
export type ContentSeo = components["schemas"]["ContentSeo"];

/** The six landing content blocks, in the order they render on the editor
 * page (D-99, `04-DATA-MODEL.md` § 6). */
export const CONTENT_KEYS: ContentKey[] = ["hero", "about", "hours", "contacts", "social", "seo"];

/** `GET /content/{key}` — requires `content.manage`. Every locale saved for
 * `key`, no fallback (O-19/O-21) — `GET /public/shop` (a later task) applies
 * the requested-locale-falls-back-to-uz rule (D-104) for the public landing. */
export async function fetchContent(key: ContentKey): Promise<ContentResource> {
  const { data, error } = await api.GET("/content/{key}", { params: { path: { key } } });
  if (error) {
    throw new ApiError(error);
  }
  return data;
}

/** `PUT /content/{key}` — requires `content.manage`. Upserts one locale;
 * `data` is validated server-side against `key`'s O-19 shape (`422
 * VALIDATION_FAILED details.fields`). Saving one locale never touches
 * another (D-104). */
export async function saveContent(key: ContentKey, body: ContentPut): Promise<ContentBlock> {
  const { data, error } = await api.PUT("/content/{key}", {
    params: { path: { key } },
    body,
  });
  if (error) {
    throw new ApiError(error);
  }
  return data;
}

/** Query-cache wrapper around `fetchContent` — one cache entry per key
 * (`["content", key]`), shared across every locale tab so switching tabs
 * never triggers a duplicate request. */
export function useContentResource(key: ContentKey) {
  return useQuery({ queryKey: ["content", key], queryFn: () => fetchContent(key) });
}
