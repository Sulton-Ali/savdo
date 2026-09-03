import { resources } from "@savdo/i18n";
import { createFileRoute } from "@tanstack/react-router";

import { getHealthz } from "../lib/api.functions";
import { healthLabel } from "../lib/health";

export const Route = createFileRoute("/")({
  loader: () => getHealthz(),
  component: Home,
});

function Home() {
  const health = Route.useLoaderData();

  return (
    <main className="flex min-h-screen flex-col items-center justify-center gap-4 bg-neutral-50 px-6 text-center">
      <h1 className="text-4xl font-bold text-neutral-900">{resources.uz.app.name}</h1>
      <p className="text-lg text-neutral-600">{healthLabel(health)}</p>
    </main>
  );
}
