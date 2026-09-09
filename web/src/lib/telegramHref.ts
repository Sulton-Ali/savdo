import type { components } from "@savdo/api-client";

import { isSafeHttpsUrl } from "./url";

type ContentSocial = components["schemas"]["ContentSocial"];

/**
 * The header, the homepage hero/CTA and the footer all need the same
 * answer to "does this shop have a usable Telegram link" — `social` is
 * `PublicShopBlocks.social` (or `undefined`/`null` when the shop has not
 * set the block at all); `null` means "no Telegram link", not an error, so
 * callers omit the button rather than render it disabled.
 */
export function getTelegramHref(social: ContentSocial | null | undefined): string | null {
  return social?.telegram != null && isSafeHttpsUrl(social.telegram) ? social.telegram : null;
}
