import type { components } from "@savdo/api-client";

import { api } from "../lib/api";
import { ApiError } from "../lib/errors";
import type { CursorPage } from "../lib/useCursorList";

export type User = components["schemas"]["User"];
export type StaffCreate = components["schemas"]["StaffCreate"];
export type StaffPatch = components["schemas"]["StaffPatch"];
export type SetStaffPassword = components["schemas"]["SetStaffPassword"];

/** Matches every other collection endpoint's default (`docs/05-API.md` §
 * Conventions). */
const PAGE_LIMIT = 50;

/** `GET /staff` — one page of the shop's staff (owner only). */
export async function fetchStaffPage(cursor: string | null): Promise<CursorPage<User>> {
  const { data, error } = await api.GET("/staff", {
    params: { query: { limit: PAGE_LIMIT, cursor: cursor ?? undefined } },
  });
  if (error) {
    throw new ApiError(error);
  }
  return data;
}

/** `POST /staff` — create a manager or cashier; the password is set here
 * (D-28). Owner only. */
export async function createStaff(body: StaffCreate): Promise<User> {
  const { data, error } = await api.POST("/staff", { body });
  if (error) {
    throw new ApiError(error);
  }
  return data;
}

/** `PATCH /staff/{id}` — partial update (fullName, phone, role, isActive,
 * locale). Owner only. */
export async function updateStaff(id: string, body: StaffPatch): Promise<User> {
  const { data, error } = await api.PATCH("/staff/{id}", {
    params: { path: { id } },
    body,
  });
  if (error) {
    throw new ApiError(error);
  }
  return data;
}

/** `POST /staff/{id}/password` — the owner resets a staff member's password
 * (D-28: no self-service reset before Phase 7). */
export async function setStaffPassword(id: string, body: SetStaffPassword): Promise<void> {
  const { error } = await api.POST("/staff/{id}/password", {
    params: { path: { id } },
    body,
  });
  if (error) {
    throw new ApiError(error);
  }
}
