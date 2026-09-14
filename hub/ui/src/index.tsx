import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router";
import { QueryClientProvider } from "@tanstack/react-query";
import { queryClient } from "@/api/client";
import { SessionProvider } from "@/context/SessionProvider";
import "@/i18n";
import App from "./App";

const root = createRoot(document.getElementById("root")!);
root.render(
  <BrowserRouter basename="/admin">
    <QueryClientProvider client={queryClient}>
      <SessionProvider>
        <App />
      </SessionProvider>
    </QueryClientProvider>
  </BrowserRouter>,
);
