import { redirect } from "@tanstack/react-router";

import type { Me } from "../../auth/api";

/**
 * Per-route `beforeLoad` guard for an owner-capability page (`/staff`,
 * `/locations`, `/settings`): redirects to `/` when `me.permissions` lacks
 * the given capability string. The API remains the enforcement point
 * (ADR-010) — this only keeps a user without the capability from seeing the
 * page shell before a request 403s.
 */
export function requirePermission(me: Me, permission: string): void {
  if (!me.permissions.includes(permission)) {
    throw redirect({ to: "/" });
  }
}
