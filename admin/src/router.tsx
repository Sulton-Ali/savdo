import { createRouter } from "@tanstack/react-router";

import { queryClient } from "./lib/queryClient";
import { authenticatedRoute } from "./routes/app/authenticatedRoute";
import { dashboardRoute } from "./routes/app/dashboardRoute";
import { locationsRoute } from "./routes/app/locationsRoute";
import { settingsRoute } from "./routes/app/settingsRoute";
import { staffRoute } from "./routes/app/staffRoute";
import { loginRoute } from "./routes/login/loginRoute";
import { rootRoute } from "./routes/root";

const routeTree = rootRoute.addChildren([
  loginRoute,
  authenticatedRoute.addChildren([dashboardRoute, staffRoute, locationsRoute, settingsRoute]),
]);

export const router = createRouter({ routeTree, context: { queryClient } });

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
