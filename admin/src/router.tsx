import { createRouter } from "@tanstack/react-router";

import { queryClient } from "./lib/queryClient";
import { attributesRoute } from "./routes/app/attributesRoute";
import { authenticatedRoute } from "./routes/app/authenticatedRoute";
import { botConversationDetailRoute } from "./routes/app/botConversationDetailRoute";
import { botConversationsRoute } from "./routes/app/botConversationsRoute";
import { categoriesRoute } from "./routes/app/categoriesRoute";
import { customerDetailRoute } from "./routes/app/customerDetailRoute";
import { customersRoute } from "./routes/app/customersRoute";
import { dashboardRoute } from "./routes/app/dashboardRoute";
import { landingContentRoute } from "./routes/app/landingContentRoute";
import { locationsRoute } from "./routes/app/locationsRoute";
import { productEditRoute } from "./routes/app/productEditRoute";
import { productNewRoute } from "./routes/app/productNewRoute";
import { productsRoute } from "./routes/app/productsRoute";
import { purchaseEditRoute } from "./routes/app/purchaseEditRoute";
import { purchaseNewRoute } from "./routes/app/purchaseNewRoute";
import { purchasesRoute } from "./routes/app/purchasesRoute";
import { quickSaleRoute } from "./routes/app/quickSaleRoute";
import { reportsRoute } from "./routes/app/reportsRoute";
import { saleDetailRoute } from "./routes/app/saleDetailRoute";
import { saleDraftDetailRoute } from "./routes/app/saleDraftDetailRoute";
import { saleDraftsListRoute } from "./routes/app/saleDraftsListRoute";
import { salesListRoute } from "./routes/app/salesListRoute";
import { settingsRoute } from "./routes/app/settingsRoute";
import { staffRoute } from "./routes/app/staffRoute";
import { stockLevelsRoute } from "./routes/app/stockLevelsRoute";
import { stockLowRoute } from "./routes/app/stockLowRoute";
import { stockMovementsRoute } from "./routes/app/stockMovementsRoute";
import { suppliersRoute } from "./routes/app/suppliersRoute";
import { telegramLinkRoute } from "./routes/app/telegramLinkRoute";
import { forgotPasswordRoute } from "./routes/forgot-password/forgotPasswordRoute";
import { loginRoute } from "./routes/login/loginRoute";
import { rootRoute } from "./routes/root";

const routeTree = rootRoute.addChildren([
  loginRoute,
  // Phase 7 T7: OTP-driven password reset (ADR-005, D-28), no session.
  forgotPasswordRoute,
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
    // Phase 4 T6b: quick sale (every role).
    quickSaleRoute,
    // Phase 4 T6c: sales list/detail (cashier+ read; void/return manager+).
    salesListRoute,
    saleDetailRoute,
    // Phase 5 T15: draft sales list/detail (cashier+ read/pay; edit/delete
    // creator or manager+, D-87..D-89, D-96). Registered before
    // `saleDetailRoute`'s dynamic `/sales/$id` is irrelevant to matching
    // (static beats dynamic either way), but keeping it next to that route
    // and `salesListRoute` groups every `/sales/*` route together.
    saleDraftsListRoute,
    saleDraftDetailRoute,
    reportsRoute,
    settingsRoute,
    // Phase 6 T4: landing content editor (manager+, D-99/O-19/O-21).
    landingContentRoute,
    stockLevelsRoute,
    stockMovementsRoute,
    stockLowRoute,
    // Phase 7 T6: bot conversations list/detail (owner/manager, `bot.read`).
    botConversationsRoute,
    botConversationDetailRoute,
    // Phase 7 T7: Telegram account link status (any authenticated role —
    // not `shop.settings`, unlike the rest of `/settings/*`).
    telegramLinkRoute,
  ]),
]);

export const router = createRouter({ routeTree, context: { queryClient } });

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
