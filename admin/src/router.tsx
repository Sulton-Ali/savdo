import { createRouter } from "@tanstack/react-router";

import { queryClient } from "./lib/queryClient";
import { attributesRoute } from "./routes/app/attributesRoute";
import { authenticatedRoute } from "./routes/app/authenticatedRoute";
import { categoriesRoute } from "./routes/app/categoriesRoute";
import { customerDetailRoute } from "./routes/app/customerDetailRoute";
import { customersRoute } from "./routes/app/customersRoute";
import { dashboardRoute } from "./routes/app/dashboardRoute";
import { locationsRoute } from "./routes/app/locationsRoute";
import { productEditRoute } from "./routes/app/productEditRoute";
import { productNewRoute } from "./routes/app/productNewRoute";
import { productsRoute } from "./routes/app/productsRoute";
import { purchaseEditRoute } from "./routes/app/purchaseEditRoute";
import { purchaseNewRoute } from "./routes/app/purchaseNewRoute";
import { purchasesRoute } from "./routes/app/purchasesRoute";
import { reportsRoute } from "./routes/app/reportsRoute";
import { settingsRoute } from "./routes/app/settingsRoute";
import { staffRoute } from "./routes/app/staffRoute";
import { stockLevelsRoute } from "./routes/app/stockLevelsRoute";
import { stockLowRoute } from "./routes/app/stockLowRoute";
import { stockMovementsRoute } from "./routes/app/stockMovementsRoute";
import { suppliersRoute } from "./routes/app/suppliersRoute";
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
    // Phase 3 T6a: suppliers (manager+).
    suppliersRoute,
    // Phase 3 T6a: purchases (manager+).
    purchasesRoute,
    purchaseNewRoute,
    purchaseEditRoute,
    // Phase 4 T6a: customers (cashier+ create/read, manager+ edit/delete).
    customersRoute,
    customerDetailRoute,
    reportsRoute,
    settingsRoute,
    stockLevelsRoute,
    stockMovementsRoute,
    stockLowRoute,
  ]),
]);

export const router = createRouter({ routeTree, context: { queryClient } });

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
