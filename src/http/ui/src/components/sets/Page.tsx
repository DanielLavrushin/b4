import { useSnackbar } from "@context/SnackbarProvider";
import { Spinner } from "@design/components/ui/spinner";
import { B4Config } from "@models/config";
import { useCallback, useEffect, useState } from "react";
import { SetsManager, SetWithStats } from "./Manager";

export function SetsPage() {
  const { showError } = useSnackbar();
  const [config, setConfig] = useState<
    (B4Config & { sets?: SetWithStats[] }) | null
  >(null);
  const [loading, setLoading] = useState(true);

  const loadConfig = useCallback(async () => {
    try {
      setLoading(true);
      const response = await fetch("/api/config");
      if (!response.ok) throw new Error("Failed to load");
      const data = (await response.json()) as B4Config & {
        sets?: SetWithStats[];
      };
      setConfig(data);
    } catch {
      showError("Failed to load configuration");
    } finally {
      setLoading(false);
    }
  }, [showError, setLoading]);

  useEffect(() => {
    void loadConfig();
  }, [loadConfig]);

  if (loading || !config) {
    return (
      <div className="fixed inset-0 z-50 flex items-center justify-center bg-background/80 backdrop-blur-sm">
        <div className="flex flex-col items-center gap-4">
          <Spinner className="h-12 w-12" />
          <p className="text-foreground">Loading...</p>
        </div>
      </div>
    );
  }

  return (
    <div className="h-full flex flex-col overflow-hidden">
      <div className="flex-1 overflow-auto">
        <SetsManager config={config} onRefresh={() => void loadConfig()} />
      </div>
    </div>
  );
}
