/**
 * The states the Settings sheet's Telegram section (deliverable D, Phase 7
 * T7) can be in, derived purely from the underlying query/mutation state —
 * no I/O, no React Native, so this is unit-testable on its own (D-73/D-85:
 * screens themselves stay untested, but the pure state logic they render
 * from does not). Mirrors `lib/sessionGate.ts`'s `deriveSessionGate`.
 */
export type TelegramLinkViewState =
  | { kind: "loading" }
  | { kind: "error" }
  | { kind: "unlinked" }
  | { kind: "linkCreated"; code: string; deepLink: string }
  | { kind: "linked"; username: string | null };

export interface TelegramLinkViewInput {
  statusPending: boolean;
  statusError: boolean;
  status: { linked: boolean; telegramUsername: string | null } | undefined;
  /** The most recent `createTelegramLink` mutation result still considered
   * current — the caller clears this (passes `undefined`) on every status
   * refresh, since a re-checked `unlinked` status makes any earlier code
   * stale (already used, or its 10 minutes ran out). */
  createdLinkCode: { code: string; deepLink: string } | undefined;
}

export function deriveTelegramLinkViewState({
  statusPending,
  statusError,
  status,
  createdLinkCode,
}: TelegramLinkViewInput): TelegramLinkViewState {
  if (statusPending) {
    return { kind: "loading" };
  }
  if (statusError || !status) {
    return { kind: "error" };
  }
  if (status.linked) {
    return { kind: "linked", username: status.telegramUsername };
  }
  if (createdLinkCode) {
    return { kind: "linkCreated", code: createdLinkCode.code, deepLink: createdLinkCode.deepLink };
  }
  return { kind: "unlinked" };
}
