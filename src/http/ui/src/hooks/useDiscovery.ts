import { useState, useCallback, useEffect, useRef } from "react";
import { ApiError, ApiResponse } from "@api/apiClient";
import { discoveryApi, DiscoverySuite, HistoryEntry } from "@b4.discovery";
import { B4SetConfig } from "@b4.sets";
import { wsUrl } from "@utils";

export function useDiscovery() {
  const [discoveryRunning, setDiscoveryRunning] = useState(false);
  const [suiteId, setSuiteId] = useState<string | null>(null);
  const [suite, setSuite] = useState<DiscoverySuite | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [history, setHistory] = useState<HistoryEntry[]>([]);
  const [historyLoading, setHistoryLoading] = useState(true);
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const initRef = useRef(false);

  const loadHistory = useCallback(async () => {
    setHistoryLoading(true);
    try {
      const entries = await discoveryApi.history();
      setHistory(entries ?? []);
    } catch {
      setHistory([]);
    } finally {
      setHistoryLoading(false);
    }
  }, []);

  // On mount: check for current running discovery and load history
  useEffect(() => {
    if (initRef.current) return;
    initRef.current = true;

    const init = async () => {
      // Check for currently running discovery
      try {
        const current = await discoveryApi.current();
        if (
          current &&
          (current.status === "running" || current.status === "pending")
        ) {
          setSuiteId(current.id);
          setSuite(current);
          setDiscoveryRunning(true);
        }
      } catch {
        // No current discovery, that's fine
      }

      // Load history
      await loadHistory();
    };

    void init();
  }, [loadHistory]);

  // Poll for status when running
  useEffect(() => {
    if (!suiteId || !discoveryRunning) return;

    const fetchStatus = async () => {
      try {
        const data = await discoveryApi.status(suiteId);
        setSuite(data);
        if (["complete", "failed", "canceled"].includes(data.status)) {
          setDiscoveryRunning(false);
          // Refresh history when discovery finishes
          void loadHistory();
        }
      } catch (e) {
        if (e instanceof ApiError && e.status === 404) {
          setDiscoveryRunning(false);
          setSuiteId(null);
          // Discovery gone from server — refresh history in case it completed
          void loadHistory();
          return;
        }
        setError(e instanceof Error ? e.message : "Unknown error");
        setDiscoveryRunning(false);
      }
    };

    // Immediate first fetch
    void fetchStatus();
    pollRef.current = setInterval(() => void fetchStatus(), 1500);
    return () => {
      if (pollRef.current) clearInterval(pollRef.current);
    };
  }, [suiteId, discoveryRunning, loadHistory]);

  const startDiscovery = useCallback(
    async (
      urls: string[],
      skipDNS: boolean = false,
      skipCache: boolean = false,
      payloadFiles: string[] = [],
      validationTries: number = 1,
      tlsVersion: string = "auto",
      ipVersion: string = "auto",
    ): Promise<ApiResponse<void>> => {
      setError(null);
      setSuite(null);
      setDiscoveryRunning(true);
      try {
        const normalized = urls
          .map((u) => u.trim())
          .filter((u) => u.length > 0)
          .map((u) =>
            u.startsWith("http://") || u.startsWith("https://")
              ? u
              : `https://${u}`,
          );
        if (normalized.length === 0) {
          setDiscoveryRunning(false);
          setSuiteId(null);
          return { success: false, error: "No URLs provided" };
        }
        const res = await discoveryApi.start(
          normalized,
          skipDNS,
          skipCache,
          payloadFiles,
          validationTries,
          tlsVersion,
          ipVersion,
        );
        setSuiteId(res.id);
        return { success: true };
      } catch (e) {
        setDiscoveryRunning(false);
        if (e instanceof ApiError) {
          return { success: false, error: JSON.stringify(e.body ?? e.message) };
        }
        return { success: false, error: String(e) };
      }
    },
    [],
  );

  const cancelDiscovery = useCallback(async (): Promise<void> => {
    if (!suiteId) return;
    try {
      await discoveryApi.cancel(suiteId);
      setDiscoveryRunning(false);
      // Refresh history after cancel
      void loadHistory();
    } catch (e) {
      console.error("Failed to cancel discovery:", e);
    }
  }, [suiteId, loadHistory]);

  const resetDiscovery = useCallback(() => {
    setSuiteId(null);
    setSuite(null);
    setError(null);
    setDiscoveryRunning(false);
  }, []);

  const addPresetAsSet = useCallback(
    async (config: B4SetConfig): Promise<ApiResponse<void>> => {
      try {
        await discoveryApi.addPresetAsSet(config);
        return { success: true };
      } catch (e) {
        if (e instanceof ApiError) {
          return { success: false, error: JSON.stringify(e.body ?? e.message) };
        }
        return { success: false, error: String(e) };
      }
    },
    [],
  );

  const clearCache = useCallback(async (): Promise<ApiResponse<void>> => {
    try {
      await discoveryApi.clearCache();
      return { success: true };
    } catch (e) {
      if (e instanceof ApiError) {
        return { success: false, error: JSON.stringify(e.body ?? e.message) };
      }
      return { success: false, error: String(e) };
    }
  }, []);

  const clearHistory = useCallback(async (): Promise<ApiResponse<void>> => {
    try {
      await discoveryApi.clearHistory();
      setHistory([]);
      return { success: true };
    } catch (e) {
      if (e instanceof ApiError) {
        return { success: false, error: JSON.stringify(e.body ?? e.message) };
      }
      return { success: false, error: String(e) };
    }
  }, []);

  const deleteHistoryDomain = useCallback(
    async (domain: string): Promise<ApiResponse<void>> => {
      try {
        await discoveryApi.deleteHistoryDomain(domain);
        setHistory((prev) => prev.filter((e) => e.domain !== domain));
        return { success: true };
      } catch (e) {
        if (e instanceof ApiError) {
          return { success: false, error: JSON.stringify(e.body ?? e.message) };
        }
        return { success: false, error: String(e) };
      }
    },
    [],
  );

  return {
    discoveryRunning,
    suiteId,
    suite,
    error,
    history,
    historyLoading,
    startDiscovery,
    cancelDiscovery,
    resetDiscovery,
    addPresetAsSet,
    clearCache,
    clearHistory,
    deleteHistoryDomain,
    loadHistory,
  };
}

const MAX_LOGS = 500;

export function useDiscoveryLogs() {
  const [logs, setLogs] = useState<string[]>([]);
  const [connected, setConnected] = useState(false);
  const wsRef = useRef<WebSocket | null>(null);
  const logsRef = useRef<string[]>([]);

  useEffect(() => {
    let ws: WebSocket | null = null;
    let reconnectTimeout: ReturnType<typeof setTimeout> | null = null;
    let isCleaningUp = false;

    const connect = () => {
      if (isCleaningUp) return;

      const url = wsUrl("/api/ws/discovery");
      ws = new WebSocket(url);
      wsRef.current = ws;

      ws.onopen = () => {
        setConnected(true);
        logsRef.current = [];
        setLogs([]);
      };

      ws.onmessage = (ev) => {
        const line = String(ev.data);
        logsRef.current = [...logsRef.current, line].slice(-MAX_LOGS);
        setLogs(logsRef.current);
      };

      ws.onerror = () => {
        setConnected(false);
      };

      ws.onclose = () => {
        setConnected(false);
        wsRef.current = null;
        if (!isCleaningUp) {
          reconnectTimeout = setTimeout(connect, 3000);
        }
      };
    };

    connect();

    return () => {
      isCleaningUp = true;
      if (reconnectTimeout) clearTimeout(reconnectTimeout);
      if (ws) ws.close();
    };
  }, []);

  const clearLogs = useCallback(() => {
    logsRef.current = [];
    setLogs([]);
  }, []);

  return { logs, connected, clearLogs };
}
