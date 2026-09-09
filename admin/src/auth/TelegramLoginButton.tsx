import { useEffect, useRef } from "react";

/**
 * The Telegram Login Widget's raw callback payload — snake_case field
 * names exactly as documented (verified against
 * https://core.telegram.org/widgets/login-legacy, the "Receiving
 * authorization data" section: "calling the callback function
 * `data-onauth` with the JSON-object containing `id`, `first_name`,
 * `last_name`, `username`, `photo_url`, `auth_date` and `hash` fields").
 * `id` and `auth_date` arrive as JS numbers here; `LoginPage` converts to
 * the contract's `TelegramAuthRequest` (`id` as a decimal string,
 * camelCase fields).
 */
export interface TelegramWidgetUser {
  id: number;
  first_name?: string;
  last_name?: string;
  username?: string;
  photo_url?: string;
  auth_date: number;
  hash: string;
}

declare global {
  interface Window {
    onTelegramAuth?: (user: TelegramWidgetUser) => void;
  }
}

/**
 * Version pinned per the task spec; the widget script is also served at
 * `?24` on the current docs page (checked live 2026-09-09) — both are the
 * same widget, the query string only busts the CDN cache for Telegram's own
 * builds, so this pin is safe.
 */
const WIDGET_SCRIPT_SRC = "https://telegram.org/js/telegram-widget.js?22";

/**
 * Renders Telegram's official Login Widget (embed snippet from
 * https://core.telegram.org/widgets/login-legacy): a `<script
 * data-telegram-login data-onauth>` tag Telegram's own JS replaces with an
 * iframe button. Mounted only by `LoginPage` (never elsewhere), so the
 * script is loaded/unloaded with this component rather than sitting on
 * every page.
 *
 * `data-onauth="onTelegramAuth(user)"` is Telegram's own convention: the
 * widget calls a *global* function by that literal name, not a React
 * prop — this component sets `window.onTelegramAuth` for the widget to
 * find, and tears it down on unmount so it can't outlive this page.
 */
export function TelegramLoginButton({
  botUsername,
  onAuth,
}: {
  botUsername: string;
  onAuth: (user: TelegramWidgetUser) => void;
}) {
  const containerRef = useRef<HTMLDivElement>(null);
  // A ref keeps the effect below from re-running (and reloading the
  // widget script) just because the caller passed a fresh inline callback
  // on every render.
  const onAuthRef = useRef(onAuth);
  onAuthRef.current = onAuth;

  useEffect(() => {
    const container = containerRef.current;
    if (!container || !botUsername) {
      return;
    }

    window.onTelegramAuth = (user) => onAuthRef.current(user);

    const script = document.createElement("script");
    script.src = WIDGET_SCRIPT_SRC;
    script.async = true;
    script.setAttribute("data-telegram-login", botUsername);
    script.setAttribute("data-size", "large");
    script.setAttribute("data-onauth", "onTelegramAuth(user)");
    container.appendChild(script);

    return () => {
      container.removeChild(script);
      delete window.onTelegramAuth;
    };
  }, [botUsername]);

  return <div ref={containerRef} data-testid="telegram-login-widget" />;
}
