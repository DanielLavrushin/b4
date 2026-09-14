import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router";
import { QueryClientProvider } from "@tanstack/react-query";
import { apiClient } from "@api/apiClient";
import "./i18n";
import App from "./App";
import { AuthProvider } from "./context/AuthProvider";
import { WebSocketProvider } from "./context/B4WsProvider";
import { AiStatusProvider } from "./context/AiStatusProvider";

const root = createRoot(document.getElementById("root")!);
root.render(
  <BrowserRouter>
    <QueryClientProvider client={apiClient}>
      <AuthProvider>
        <WebSocketProvider>
          <AiStatusProvider>
            <App />
          </AiStatusProvider>
        </WebSocketProvider>
      </AuthProvider>
    </QueryClientProvider>
  </BrowserRouter>,
);
