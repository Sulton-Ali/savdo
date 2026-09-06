import type { components } from "@savdo/api-client";

/**
 * Pure, RN-free helpers for sale drafts (D-85..D-90, T14) — no React or
 * React Native import, same D-85 contract as `features/sales/cart.ts` and
 * `features/purchases/draft.ts`, so this is testable under plain Vitest.
 * Request/response bodies themselves live in `features/sales/cart.ts`
 * (`buildDraftCreateBody`/`buildDraftPatchBody`) and `features/sales/api.ts`
 * (the actual `fetch` calls); this module is the smaller pieces those two
 * don't own: the creator-or-manager+ permission check, the "which line
 * failed" error parser, and the list row's age.
 */

type Role = components["schemas"]["Role"];

/**
 * `PATCH`/`DELETE /sales/drafts/{id}` require `cashier+` *and* either the
 * draft's own creator or `manager+` (D-89, `docs/05-API.md`'s drafts rows);
 * `POST .../complete` (Pay) has no such restriction (D-96: any staff who
 * can create a sale may complete any draft) and must never be gated by
 * this predicate — `drafts/[id].tsx` only calls this for its Edit/Delete
 * actions. `createdBy` is `null` for a draft with no creator on record,
 * editable/deletable only by manager+ in that case
 * (`SaleDraft.createdBy`'s own doc comment). The API remains the actual
 * enforcement point (ADR-010) — this only decides whether the UI shows
 * the buttons.
 */
export function canManageDraft(
  role: Role | undefined,
  userId: string | undefined,
  createdBy: string | null,
): boolean {
  if (role === "owner" || role === "manager") {
    return true;
  }
  return createdBy != null && userId != null && createdBy === userId;
}

/** Matches a `422 VALIDATION_FAILED` `details.fields` key naming an
 * unavailable draft line at `POST /sales/drafts/{id}/complete` —
 * `items[<i>].variantId` (`api/internal/sales/drafts_write.go`'s
 * `errDraftLineUnavailable`, `docs/05-API.md`'s complete row). */
const UNAVAILABLE_LINE_FIELD_RE = /^items\[(\d+)\]\.variantId$/;

/**
 * Extracts every draft line index a `VALIDATION_FAILED` error's
 * `details.fields` named as unavailable, ascending — `fields` is a
 * `field: reason` map (`docs/05-API.md` § Conventions, O-12), so a
 * completion that fails on more than one line reports all of them at
 * once. Returns `[]` for `undefined`/`{}` or a `fields` object with no
 * matching key, so a caller can tell "no line named" (show the generic
 * error instead) apart from "these lines are the problem".
 */
export function parseUnavailableLineIndexes(fields: Record<string, string> | undefined): number[] {
  if (!fields) {
    return [];
  }
  const indexes: number[] = [];
  for (const key of Object.keys(fields)) {
    const match = UNAVAILABLE_LINE_FIELD_RE.exec(key);
    if (match?.[1] != null) {
      indexes.push(Number(match[1]));
    }
  }
  return indexes.sort((a, b) => a - b);
}

export type DraftAgeUnit = "minutes" | "hours" | "days";

export interface DraftAge {
  unit: DraftAgeUnit;
  value: number;
}

/**
 * A draft's age for the drafts list row (deliverable 2) — `createdAt`
 * (ISO date-time) against `now` (defaults to the real clock; a caller
 * passes a fixed `now` for a deterministic test). Never negative — a
 * `createdAt` briefly in the future (clock skew between the phone and the
 * server) reports `{ unit: "minutes", value: 0 }` rather than a negative
 * count. The screen renders this via `t("mobile.drafts.age.<unit>",
 * { count: value })`, letting i18next's own plural rules per locale
 * handle "0 min ago" vs "1 min ago" vs "5 min ago".
 */
export function draftAge(createdAt: string, now: Date = new Date()): DraftAge {
  const diffMs = Math.max(0, now.getTime() - new Date(createdAt).getTime());
  const minutes = Math.floor(diffMs / 60_000);
  if (minutes < 60) {
    return { unit: "minutes", value: minutes };
  }
  const hours = Math.floor(minutes / 60);
  if (hours < 24) {
    return { unit: "hours", value: hours };
  }
  return { unit: "days", value: Math.floor(hours / 24) };
}
