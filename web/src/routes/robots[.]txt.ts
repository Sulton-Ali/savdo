import { createFileRoute } from "@tanstack/react-router";
import { getSiteUrlValue } from "../lib/site.server";
import { buildRobotsTxt } from "../lib/sitemap";

/** O-17: allows every crawler and points at `/sitemap.xml`. */
export const Route = createFileRoute("/robots.txt")({
  server: {
    handlers: {
      GET: async () => {
        const siteUrl = getSiteUrlValue();
        return new Response(buildRobotsTxt(siteUrl), {
          headers: { "Content-Type": "text/plain; charset=utf-8" },
        });
      },
    },
  },
});
