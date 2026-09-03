import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { App as AntApp } from "antd";
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
        {/* Ant Design's `App` context (`App.useApp()`) — the page-level
         * notification surface for errors `applyApiErrorToForm` doesn't
         * turn into a field error (`lib/errors.ts`, ADR-013). */}
        <AntApp>
          <RouterProvider router={router} />
        </AntApp>
      </AppConfigProvider>
    </QueryClientProvider>
  </StrictMode>,
);
