import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { createTelegramLink, deleteTelegramLink, fetchTelegramLinkStatus } from "@/lib/authApi";
import { accountKeys } from "@/lib/queryKeys";

/** The caller's own Telegram link status (`GET /auth/telegram/link`,
 * Phase 7 T7 deliverable D). */
export function useTelegramLinkStatus() {
  return useQuery({
    queryKey: accountKeys.telegramLink(),
    queryFn: fetchTelegramLinkStatus,
  });
}

/**
 * Starts linking the caller's account (`POST /auth/telegram/link`). The
 * returned code/deep link is mutation state only, never written to the
 * `accountKeys.telegramLink()` query cache — it's single-use and goes stale
 * the moment a status refresh reports linked (or the 10 minutes run out).
 */
export function useCreateTelegramLink() {
  return useMutation({ mutationFn: createTelegramLink });
}

/** Unlinks the caller's account (`DELETE /auth/telegram/link`) and
 * refreshes the status. */
export function useDeleteTelegramLink() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: deleteTelegramLink,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: accountKeys.telegramLink() }),
  });
}
