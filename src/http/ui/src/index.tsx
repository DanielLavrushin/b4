import { createRoot } from "react-dom/client";
import { createBrowserRouter, RouterProvider } from "react-router";
import { QueryClientProvider } from "@tanstack/react-query";
import { apiClient } from "@api/apiClient";
import "./i18n";
import App, { appRoutes } from "./App";
import { AuthProvider } from "./context/AuthProvider";
import { WebSocketProvider } from "./context/B4WsProvider";
import { AiStatusProvider } from "./context/AiStatusProvider";

const router = createBrowserRouter([
  { path: "/", element: <App />, children: appRoutes },
]);
const root = createRoot(document.getElementById("root")!);
root.render(
  <QueryClientProvider client={apiClient}>
    <AuthProvider>
      <WebSocketProvider>
        <AiStatusProvider>
          <RouterProvider router={router} />
        </AiStatusProvider>
      </WebSocketProvider>
    </AuthProvider>
  </QueryClientProvider>,
);
