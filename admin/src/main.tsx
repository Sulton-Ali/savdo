import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import "./index.css";
// Initialises i18next before anything renders (ADR-012, D-31).
import "./i18n";
import { AppConfigProvider } from "./components/AppConfigProvider";
import { queryClient } from "./lib/queryClient";
import { router } from "./router";

const rootElement = document.getElementById("root");
if (!rootElement) {
  throw new Error("#root element not found");
}

createRoot(rootElement).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <AppConfigProvider>
        <RouterProvider router={router} />
      </AppConfigProvider>
    </QueryClientProvider>
  </StrictMode>,
);
