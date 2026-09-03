import type { components } from "@savdo/api-client";

import { api } from "../lib/api";
import { ApiError } from "../lib/errors";
import type { CursorPage } from "../lib/useCursorList";

export type Location = components["schemas"]["Location"];
export type LocationCreate = components["schemas"]["LocationCreate"];
export type LocationPatch = components["schemas"]["LocationPatch"];

const PAGE_LIMIT = 50;

/** `GET /locations` — one page of the shop's locations. */
export async function fetchLocationsPage(cursor: string | null): Promise<CursorPage<Location>> {
  const { data, error } = await api.GET("/locations", {
    params: { query: { limit: PAGE_LIMIT, cursor: cursor ?? undefined } },
  });
  if (error) {
    throw new ApiError(error);
  }
  return data;
}

/** `POST /locations` — create a store or warehouse location. Owner only. */
export async function createLocation(body: LocationCreate): Promise<Location> {
  const { data, error } = await api.POST("/locations", { body });
  if (error) {
    throw new ApiError(error);
  }
  return data;
}

/** `PATCH /locations/{id}` — partial update (name, kind, isDefault,
 * isActive). Owner only. */
export async function updateLocation(id: string, body: LocationPatch): Promise<Location> {
  const { data, error } = await api.PATCH("/locations/{id}", {
    params: { path: { id } },
    body,
  });
  if (error) {
    throw new ApiError(error);
  }
  return data;
}
