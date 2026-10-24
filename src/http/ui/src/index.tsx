import { createRoot } from "react-dom/client";
import { createBrowserRouter, RouterProvider } from "react-router";
import { QueryClientProvider } from "@tanstack/react-query";
import { apiClient } from "@api/apiClient";
import "./i18n";
import App, { appRoutes } from "./App";
import { AuthProvider } from "./context/AuthProvider";
import { WebSocketProvider } from "./context/B4WsProvider";
import { AiStatusProvider } from "./context/AiStatusProvider";

const CHUNK_RELOAD_KEY = "b4_chunk_reload_at";

window.addEventListener("vite:preloadError", () => {
  try {
    const last = Number(sessionStorage.getItem(CHUNK_RELOAD_KEY) ?? 0);
    if (Date.now() - last < 10_000) return;
    sessionStorage.setItem(CHUNK_RELOAD_KEY, String(Date.now()));
  } catch {
    return;
  }
  window.location.reload();
});

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
