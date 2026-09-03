import { createRoute } from "@tanstack/react-router";

import { HomePage } from "./HomePage";
import { rootRoute } from "./root";

export const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  component: HomePage,
});
