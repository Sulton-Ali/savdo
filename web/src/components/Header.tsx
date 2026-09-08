import { Link } from "@tanstack/react-router";

import type { Locale } from "../lib/locale";

export function Header({ shopName, locale }: { shopName: string; locale: Locale }) {
  return (
    <header className="border-bg border-b bg-surface">
      <div className="mx-auto flex max-w-5xl items-center px-4 py-4">
        <Link to="/$locale" params={{ locale }} className="font-semibold text-lg text-text">
          {shopName}
        </Link>
      </div>
    </header>
  );
}
