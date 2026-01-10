import {
  SidebarInset,
  SidebarProvider,
  SidebarTrigger,
} from "@design/components/ui/sidebar";
import { Navigate, Route, Routes } from "react-router-dom";
import { AppSidebar } from "./components/common/AppSidebar";
import { ConnectionsPage } from "./components/connections/Page";
import { DashboardPage } from "./components/dashboard/Page";
import { DiscoveryPage } from "./components/discovery/Page";
import { LogsPage } from "./components/logs/Page";
import { SetsPage } from "./components/sets/Page";
import { SettingsPage } from "./components/settings/Page";
import { SnackbarProvider } from "./context/SnackbarProvider";

export function App() {
  return (
    <SidebarProvider>
      <AppSidebar />
      <SidebarInset>
        <header className="flex h-16 shrink-0 items-center gap-2 border-b px-4">
          <SidebarTrigger className="-ml-1" />
        </header>
        <div className="flex flex-1 flex-col overflow-auto p-6">
          <SnackbarProvider>
            <Routes>
              <Route path="/" element={<Navigate to="/dashboard" replace />} />
              <Route path="/dashboard" element={<DashboardPage />} />
              <Route path="/connections" element={<ConnectionsPage />} />
              <Route path="/sets" element={<SetsPage />} />
              <Route path="/settings/*" element={<SettingsPage />} />
              <Route path="/discovery" element={<DiscoveryPage />} />
              <Route path="/logs" element={<LogsPage />} />
            </Routes>
          </SnackbarProvider>
        </div>
      </SidebarInset>
    </SidebarProvider>
  );
}

export default App;
