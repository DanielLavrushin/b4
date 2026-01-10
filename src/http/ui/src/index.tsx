import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import App from "./App";
import { WebSocketProvider } from "./context/B4WsProvider";
import "./index.css";

const rootElement = document.getElementById("root");
if (!rootElement) {
  throw new Error("Root element not found");
}

const root = createRoot(rootElement);

try {
  root.render(
    <BrowserRouter>
      <WebSocketProvider>
        <App />
      </WebSocketProvider>
    </BrowserRouter>
  );
} catch (error) {
  console.error("Failed to render app:", error);
  root.render(
    <div style={{ padding: "20px", color: "red" }}>
      <h1>Error loading application</h1>
      <pre>{String(error)}</pre>
    </div>
  );
}
