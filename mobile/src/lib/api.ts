import { createClient } from "@savdo/api-client";

/**
 * The one typed Savdo API client for the mobile app (ADR-002, AGENTS.md hard
 * rule 6). Never call `fetch` directly anywhere else in `mobile/src`.
 *
 * Base URL defaults to `10.0.2.2`, the Android-emulator alias for the host
 * machine's `localhost` — a real device on the same network needs
 * `EXPO_PUBLIC_API_URL` set to the host's LAN IP (`EXPO_PUBLIC_*` is the only
 * env prefix Expo exposes to app code).
 */
export const api = createClient(process.env.EXPO_PUBLIC_API_URL ?? "http://10.0.2.2:8080/v1");
