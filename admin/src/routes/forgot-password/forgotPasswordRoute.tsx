import { createRoute } from "@tanstack/react-router";

import { rootRoute } from "../root";
import { ForgotPasswordPage } from "./ForgotPasswordPage";

export const forgotPasswordRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/forgot-password",
  component: ForgotPasswordPage,
});
