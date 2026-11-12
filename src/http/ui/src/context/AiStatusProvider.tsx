import {
  createContext,
  use,
  useCallback,
  useEffect,
  useMemo,
  useState,
  ReactNode,
} from "react";
import { aiApi, AIStatus } from "@api/ai";
import { useAuth } from "./AuthProvider";

interface AiStatusContextType {
  status: AIStatus | null;
  loading: boolean;
  enabled: boolean;
  ready: boolean;
  refresh: () => Promise<AIStatus | null>;
}

const AiStatusContext = createContext<AiStatusContextType | null>(null);

export function AiStatusProvider({ children }: Readonly<{ children: ReactNode }>) {
  const [status, setStatus] = useState<AIStatus | null>(null);
  const [loading, setLoading] = useState(false);
  const { hasSession } = useAuth();

  const refresh = useCallback(async (): Promise<AIStatus | null> => {
    try {
      setLoading(true);
      const data = await aiApi.status();
      setStatus(data);
      return data;
    } catch (err) {
      console.error("ai status failed", err);
      return null;
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (hasSession) void refresh();
  }, [hasSession, refresh]);

  const value = useMemo<AiStatusContextType>(
    () => ({
      status,
      loading,
      enabled: Boolean(status?.enabled),
      ready: Boolean(status?.ready),
      refresh,
    }),
    [status, loading, refresh],
  );

  return <AiStatusContext value={value}>{children}</AiStatusContext>;
}

export function useAiStatus(): AiStatusContextType {
  const ctx = use(AiStatusContext);
  if (!ctx) {
    throw new Error("useAiStatus must be used within AiStatusProvider");
  }
  return ctx;
}
