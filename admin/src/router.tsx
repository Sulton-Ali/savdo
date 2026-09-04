import { createRouter } from "@tanstack/react-router";

import { queryClient } from "./lib/queryClient";
import { attributesRoute } from "./routes/app/attributesRoute";
import { authenticatedRoute } from "./routes/app/authenticatedRoute";
import { categoriesRoute } from "./routes/app/categoriesRoute";
import { dashboardRoute } from "./routes/app/dashboardRoute";
import { locationsRoute } from "./routes/app/locationsRoute";
import { productEditRoute } from "./routes/app/productEditRoute";
import { productNewRoute } from "./routes/app/productNewRoute";
import { productsRoute } from "./routes/app/productsRoute";
import { settingsRoute } from "./routes/app/settingsRoute";
import { staffRoute } from "./routes/app/staffRoute";
import { loginRoute } from "./routes/login/loginRoute";
import { rootRoute } from "./routes/root";

const routeTree = rootRoute.addChildren([
  loginRoute,
  authenticatedRoute.addChildren([
    dashboardRoute,
    productsRoute,
    productNewRoute,
    productEditRoute,
    categoriesRoute,
    attributesRoute,
    staffRoute,
    locationsRoute,
    settingsRoute,
  ]),
]);

export const router = createRouter({ routeTree, context: { queryClient } });

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
