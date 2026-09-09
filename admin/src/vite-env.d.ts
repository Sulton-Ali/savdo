/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_API_BASE?: string;
  /** Telegram bot username for the Login Widget (Phase 7 T7,
   * docs/07-DEVOPS.md § Environment variables). Empty/unset hides "Log in
   * with Telegram" on the login page. */
  readonly VITE_BOT_USERNAME?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
