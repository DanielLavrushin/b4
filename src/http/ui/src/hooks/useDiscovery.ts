import { useState, useCallback, useEffect, useRef } from "react";
import { ApiError, ApiResponse } from "@api/apiClient";
import {
  AddPresetResult,
  DiscoveryStartOptions,
  discoveryApi,
} from "@api/discovery";
import { DiscoverySuite, HistoryEntry, isSuite } from "@models/discovery";
import { B4SetConfig } from "@models/config";
import { wsUrl, describeApiError } from "@utils";

const POLL_MS = 1500;
const TERMINAL = new Set(["complete", "failed", "canceled"]);

const failureText = (e: unknown): string => {
  if (e instanceof ApiError) {
    const detail = typeof e.body === "string" ? e.body.trim() : "";
    return detail.length > 0 ? detail : e.message;
  }
  return e instanceof Error ? e.message : String(e);
};

export function useDiscovery() {
  const [running, setRunning] = useState(false);
  const [finishing, setFinishing] = useState(false);
  const [stopping, setStopping] = useState(false);
  const [suiteId, setSuiteId] = useState<string | null>(null);
  const [suite, setSuite] = useState<DiscoverySuite | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [history, setHistory] = useState<HistoryEntry[]>([]);
  const [historyLoading, setHistoryLoading] = useState(true);
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

  useEffect(() => {
    if (initRef.current) return;
    initRef.current = true;

    const init = async () => {
      try {
        const current = await discoveryApi.current();
        if (isSuite(current)) {
          if (current.status === "running" || current.status === "pending") {
            setSuiteId(current.id);
            setSuite(current);
            setRunning(true);
          }
        } else if (current?.runtime_active) {
          setFinishing(true);
        }
      } catch {
        setFinishing(false);
      }
      await loadHistory();
    };

    void init();
  }, [loadHistory]);

  useEffect(() => {
    if (!running && !finishing) return;

    const tick = async () => {
      if (!suiteId) {
        try {
          const current = await discoveryApi.current();
          if (isSuite(current)) {
            setSuiteId(current.id);
            setSuite(current);
            setRunning(true);
            setFinishing(false);
          } else if (!current?.runtime_active) {
            setFinishing(false);
            void loadHistory();
          }
        } catch {
          setFinishing(false);
        }
        return;
      }
      try {
        const data = await discoveryApi.status(suiteId);
        setSuite(data);
        if (!TERMINAL.has(data.status)) return;
        setRunning(false);
        setStopping(false);
        if (data.runtime_active) {
          setFinishing(true);
        } else {
          setFinishing(false);
          void loadHistory();
        }
      } catch (e) {
        setRunning(false);
        setStopping(false);
        setFinishing(false);
        if (e instanceof ApiError && e.status === 404) {
          void loadHistory();
          return;
        }
        setError(failureText(e));
      }
    };

    void tick();
    const timer = setInterval(() => void tick(), POLL_MS);
    return () => clearInterval(timer);
  }, [suiteId, running, finishing, loadHistory]);

  const startDiscovery = useCallback(
    async (
      urls: string[],
      options: DiscoveryStartOptions,
    ): Promise<ApiResponse<void>> => {
      const normalized = urls
        .map((u) => u.trim())
        .filter((u) => u.length > 0)
        .map((u) =>
          u.startsWith("http://") || u.startsWith("https://")
            ? u
            : `https://${u}`,
        );
      if (normalized.length === 0) {
        return { success: false, error: "No sites given" };
      }
      setError(null);
      setSuite(null);
      setSuiteId(null);
      setStopping(false);
      setRunning(true);
      try {
        const res = await discoveryApi.start(normalized, options);
        setSuiteId(res.id);
        return { success: true };
      } catch (e) {
        setRunning(false);
        const message = failureText(e);
        setError(message);
        return { success: false, error: message };
      }
    },
    [],
  );

  const cancelDiscovery = useCallback(async (): Promise<void> => {
    if (!suiteId) return;
    setStopping(true);
    try {
      await discoveryApi.cancel(suiteId);
    } catch (e) {
      setStopping(false);
      setError(failureText(e));
    }
  }, [suiteId]);

  const resetDiscovery = useCallback(() => {
    setSuiteId(null);
    setSuite(null);
    setError(null);
    setStopping(false);
    setRunning(false);
  }, []);

  const addPresetAsSet = useCallback(
    async (config: B4SetConfig): Promise<ApiResponse<AddPresetResult>> => {
      try {
        const res = await discoveryApi.addPresetAsSet(config);
        return { success: true, data: res };
      } catch (e) {
        return { success: false, error: describeApiError(e) };
      }
    },
    [],
  );

  const clearCache = useCallback(async (): Promise<ApiResponse<void>> => {
    try {
      await discoveryApi.clearCache();
      return { success: true };
    } catch (e) {
      return { success: false, error: failureText(e) };
    }
  }, []);

  const clearHistory = useCallback(async (): Promise<ApiResponse<void>> => {
    try {
      await discoveryApi.clearHistory();
      setHistory([]);
      return { success: true };
    } catch (e) {
      return { success: false, error: failureText(e) };
    }
  }, []);

  const deleteHistoryDomain = useCallback(
    async (domain: string): Promise<ApiResponse<void>> => {
      try {
        await discoveryApi.deleteHistoryDomain(domain);
        setHistory((prev) => prev.filter((e) => e.domain !== domain));
        return { success: true };
      } catch (e) {
        return { success: false, error: failureText(e) };
      }
    },
    [],
  );

  return {
    running,
    finishing,
    stopping,
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

const MAX_LOGS = 4000;

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
