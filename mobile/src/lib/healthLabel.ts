/** Pure formatter for the healthz screen state — kept separate from the query so it is trivially testable. */
export function healthLabel(status: "ok" | undefined): string {
  return status === "ok" ? "API: ok" : "API: unknown";
}
