/** Pure helpers for the admin drafts page (`/sales/drafts`, D-87..D-89, D-96)
 * — kept free of React/Ant Design/i18next so they're plain-function
 * testable (`docs/06-ROADMAP.md` Phase 5 draft box). */

/** A draft's age bucket, coarse enough for a "created 5 minutes ago" label.
 * `justNow` covers under a minute; each other bucket floors to whole units
 * (5m59s is still "5 minutes"). */
export type DraftAgeUnit = "justNow" | "minutes" | "hours" | "days";

export interface DraftAge {
  unit: DraftAgeUnit;
  /** Always `0` for `justNow`; otherwise the whole-unit count an i18next
   * `_one`/`_few`/`_many`/`_other` key interpolates as `{{count}}`. */
  value: number;
}

const MINUTE_MS = 60_000;
const HOUR_MS = 60 * MINUTE_MS;
const DAY_MS = 24 * HOUR_MS;

/** Buckets `createdAt` (an RFC 3339 timestamp, `docs/05-API.md` §
 * Conventions) against `now` (defaults to the real clock) into the coarse
 * unit the drafts list renders next to each row's created time. A
 * `createdAt` in the future (clock skew) is clamped to `justNow` rather
 * than going negative. */
export function draftAge(createdAt: string, now: Date = new Date()): DraftAge {
  const diffMs = Math.max(0, now.getTime() - new Date(createdAt).getTime());
  if (diffMs < MINUTE_MS) {
    return { unit: "justNow", value: 0 };
  }
  if (diffMs < HOUR_MS) {
    return { unit: "minutes", value: Math.floor(diffMs / MINUTE_MS) };
  }
  if (diffMs < DAY_MS) {
    return { unit: "hours", value: Math.floor(diffMs / HOUR_MS) };
  }
  return { unit: "days", value: Math.floor(diffMs / DAY_MS) };
}

/** Matches a `VALIDATION_FAILED`/`422` `details.fields` key naming one
 * draft line, e.g. `"items[2].variantId"` (`docs/05-API.md` §
 * Conventions, `POST /sales/drafts/{id}/complete`'s 422 naming an
 * unavailable line, D-96). */
const ITEM_VARIANT_FIELD = /^items\[(\d+)\]\.variantId$/;

export interface DraftLineFieldMatch {
  /** Zero-based index into `SaleDraft.items`. */
  index: number;
  /** The validation-vocabulary reason word (`docs/05-API.md` § Conventions
   * — `required`, `invalid`, ...). */
  reason: string;
}

/** Finds the first `items[<index>].variantId` field in a `VALIDATION_FAILED`
 * error's `details.fields`, so the caller can name the offending line
 * instead of showing a generic message. Returns `null` when no such field
 * is present (a validation error on an unrelated field). */
export function findUnavailableLineField(
  fields: Record<string, string> | undefined,
): DraftLineFieldMatch | null {
  if (!fields) {
    return null;
  }
  for (const [field, reason] of Object.entries(fields)) {
    const match = ITEM_VARIANT_FIELD.exec(field);
    if (match?.[1] != null) {
      return { index: Number(match[1]), reason };
    }
  }
  return null;
}

/** Creator-or-manager+ predicate (D-89): a draft is editable/deletable by
 * its own creator, or by anyone with the manager+ capability, regardless
 * of creator — including a draft whose `createdBy` is `null` (no creator
 * on record), which only manager+ may touch. `canManage` is the caller's
 * `sales.void` capability (manager+ in the permission matrix,
 * `docs/04-DATA-MODEL.md` § 7) — reused here rather than a new capability
 * string, matching how `SaleDetailPage` gates void/return. */
export function canManageDraft(
  createdBy: string | null,
  currentUserId: string,
  canManage: boolean,
): boolean {
  if (canManage) {
    return true;
  }
  return createdBy !== null && createdBy === currentUserId;
}
