import { useState, useCallback, useEffect, useRef } from "react";
import { ApiError, ApiResponse } from "@api/apiClient";
import {
  AddPresetResult,
  DiscoveryStartOptions,
  ReplaceStrategyOptions,
  ReplaceStrategyResult,
  discoveryApi,
} from "@api/discovery";
import {
  DSCPPath,
  DiscoverySuite,
  HistoryEntry,
  isSuite,
} from "@models/discovery";
import { B4SetConfig } from "@models/config";
import { MAX_PROBE_URLS, wsUrl, describeApiError } from "@utils";
import i18n from "@/i18n";
import { useAuth, verifySession } from "@context/AuthProvider";
import {
  forgetRun,
  observeRun,
  showOnPage,
  watchRun,
} from "@/stores/discoveryWatch";

const POLL_MS = 1500;
const IDLE_POLL_MS = 5000;
const TERMINAL = new Set(["complete", "failed", "canceled"]);
const DISMISSED_RUN_KEY = "b4_discovery_dismissed_run";

const isLive = (run: DiscoverySuite): boolean =>
  run.status === "running" || run.status === "pending";

const dismissedRun = (): string | null => {
  try {
    return localStorage.getItem(DISMISSED_RUN_KEY);
  } catch {
    return null;
  }
};

const dismissRun = (id: string) => {
  try {
    localStorage.setItem(DISMISSED_RUN_KEY, id);
  } catch {
    return;
  }
};

const failureText = (e: unknown): string => {
  if (e instanceof ApiError) {
    const detail = typeof e.body === "string" ? e.body.trim() : "";
    return detail.length > 0 ? detail : e.message;
  }
  return e instanceof Error ? e.message : String(e);
};

const START_ERROR_CODES = new Set([
  "not_found",
  "no_urls",
  "too_many_urls",
  "reserved_host",
  "routed_set",
  "bad_dns_server",
  "bad_pin",
  "dscp_rules_pending",
]);

const startFailureText = (e: unknown): string => {
  if (e instanceof ApiError && e.code && START_ERROR_CODES.has(e.code)) {
    return i18n.t(`discovery.errors.${e.code}`, { max: MAX_PROBE_URLS });
  }
  return failureText(e);
};

export function useDiscovery({
  restoreLast = true,
}: { restoreLast?: boolean } = {}) {
  const [running, setRunning] = useState(false);
  const [finishing, setFinishing] = useState(false);
  const [stopping, setStopping] = useState(false);
  const [finishRequested, setFinishRequested] = useState(false);
  const [suiteId, setSuiteId] = useState<string | null>(null);
  const [suite, setSuite] = useState<DiscoverySuite | null>(null);
  const [otherRun, setOtherRun] = useState<DiscoverySuite | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [history, setHistory] = useState<HistoryEntry[]>([]);
  const [historyLoading, setHistoryLoading] = useState(true);
  const initRef = useRef(false);
  const seenRunRef = useRef<string | null>(null);
  const restoredRef = useRef<string | null>(null);
  const runGenRef = useRef(0);
  const pollErrorRef = useRef<string | null>(null);
  const mountedRef = useRef(false);

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);

  useEffect(() => showOnPage(suiteId), [suiteId]);

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

  const followRun = useCallback((run: DiscoverySuite) => {
    setOtherRun(null);
    setSuiteId(run.id);
    setSuite(run);
    setRunning(true);
    setFinishing(false);
  }, []);

  const meetLiveRun = useCallback(
    (live: DiscoverySuite, shown: string | null) => {
      observeRun(live);
      if (shown === live.id) return;
      if (!shown || shown === restoredRef.current) {
        followRun(live);
        return;
      }
      setOtherRun((known) => (known?.id === live.id ? known : live));
    },
    [followRun],
  );

  const load = useCallback(
    async (asked: string | null, blank: boolean, restore: boolean) => {
      const gen = runGenRef.current;
      const stale = () => !mountedRef.current || runGenRef.current !== gen;

      const finished = async (
        id: string | null | undefined,
        wanted: boolean,
      ): Promise<DiscoverySuite | null> => {
        if (!id || (!wanted && dismissedRun() === id)) return null;
        try {
          const data = await discoveryApi.status(id);
          if (!TERMINAL.has(data.status)) return null;
          if (data.source && data.source !== "web") return null;
          return data;
        } catch {
          return null;
        }
      };

      try {
        const current = await discoveryApi.current();
        if (stale()) return;
        const live = isSuite(current) && isLive(current) ? current : null;
        const idle = isSuite(current) ? null : current;
        if (live) observeRun(live);
        if (idle) seenRunRef.current = idle.last_run ?? null;

        if (asked && asked !== live?.id) {
          const result = await finished(asked, true);
          if (stale()) return;
          if (result) {
            observeRun(result, { shown: true });
            restoredRef.current = null;
            setError(null);
            setRunning(false);
            setStopping(false);
            setFinishRequested(false);
            setSuiteId(result.id);
            setSuite(result);
            setOtherRun(live);
            setFinishing(!!idle?.runtime_active);
            return;
          }
        }
        if (!blank) return;
        if (live) {
          followRun(live);
          return;
        }
        const last = restore ? await finished(idle?.last_run, false) : null;
        if (stale()) return;
        if (last) {
          observeRun(last, { shown: true });
          restoredRef.current = last.id;
          setSuiteId(last.id);
          setSuite(last);
        }
        if (last ? last.runtime_active : idle?.runtime_active) {
          setFinishing(true);
        }
      } catch {
        if (blank && !stale()) setFinishing(false);
      }
    },
    [followRun],
  );

  useEffect(() => {
    if (initRef.current) return;
    initRef.current = true;

    const init = async () => {
      await load(null, true, restoreLast);
      await loadHistory();
    };

    void init();
  }, [load, loadHistory, restoreLast]);

  const openRun = useCallback(
    (id: string) => {
      runGenRef.current += 1;
      void load(id, !suiteId && !running, restoreLast);
    },
    [load, suiteId, running, restoreLast],
  );

  useEffect(() => {
    if (!running && !finishing) return;
    let active = true;

    const awaitRuntime = async () => {
      try {
        const current = await discoveryApi.current();
        if (!active) return;
        if (isSuite(current) && isLive(current)) {
          meetLiveRun(current, suiteId);
          setFinishing(false);
          if (!running) void loadHistory();
        } else if (!current?.runtime_active) {
          setOtherRun(null);
          setFinishing(false);
          void loadHistory();
        }
      } catch {
        if (active) setFinishing(false);
      }
    };

    const tick = async () => {
      if (!suiteId || !running) {
        await awaitRuntime();
        return;
      }
      try {
        const data = await discoveryApi.status(suiteId);
        if (!active) return;
        const shownPollError = pollErrorRef.current;
        if (shownPollError !== null) {
          pollErrorRef.current = null;
          setError((current) => (current === shownPollError ? null : current));
        }
        observeRun(data, { shown: true });
        setSuite(data);
        if (!TERMINAL.has(data.status)) return;
        setRunning(false);
        setStopping(false);
        setFinishRequested(false);
        if (data.runtime_active) {
          setFinishing(true);
        } else {
          setFinishing(false);
          void loadHistory();
        }
      } catch (e) {
        if (!active) return;
        if (e instanceof ApiError && e.status === 404) {
          setRunning(false);
          setStopping(false);
          setFinishRequested(false);
          setFinishing(false);
          forgetRun(suiteId);
          setSuite(null);
          setSuiteId(null);
          void loadHistory();
          return;
        }
        const message = failureText(e);
        pollErrorRef.current = message;
        setError(message);
      }
    };

    void tick();
    const timer = setInterval(() => void tick(), POLL_MS);
    return () => {
      active = false;
      clearInterval(timer);
    };
  }, [suiteId, running, finishing, meetLiveRun, loadHistory]);

  useEffect(() => {
    if (running || finishing) return;
    let active = true;

    const tick = async () => {
      try {
        const current = await discoveryApi.current();
        if (!active) return;
        if (isSuite(current)) {
          if (isLive(current)) meetLiveRun(current, suiteId);
          else setOtherRun(null);
          return;
        }
        setOtherRun(null);
        const last = current?.last_run;
        if (current?.runtime_active) {
          if (last) seenRunRef.current = last;
          setFinishing(true);
          return;
        }
        if (last && last !== seenRunRef.current) {
          seenRunRef.current = last;
          if (last !== suiteId) void loadHistory();
        }
      } catch {
        return;
      }
    };

    const onVisible = () => {
      if (!document.hidden) void tick();
    };
    const timer = setInterval(() => void tick(), IDLE_POLL_MS);
    document.addEventListener("visibilitychange", onVisible);
    return () => {
      active = false;
      clearInterval(timer);
      document.removeEventListener("visibilitychange", onVisible);
    };
  }, [running, finishing, suiteId, meetLiveRun, loadHistory]);

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
      runGenRef.current += 1;
      setError(null);
      setSuite(null);
      setSuiteId(null);
      setOtherRun(null);
      setStopping(false);
      setFinishRequested(false);
      setRunning(true);
      try {
        const res = await discoveryApi.start(normalized, options);
        watchRun(res.id);
        setSuiteId(res.id);
        return { success: true };
      } catch (e) {
        setRunning(false);
        const message = startFailureText(e);
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

  const finishDiscovery = useCallback(async (): Promise<void> => {
    if (!suiteId) return;
    setFinishRequested(true);
    try {
      await discoveryApi.finish(suiteId);
    } catch (e) {
      setFinishRequested(false);
      setError(failureText(e));
    }
  }, [suiteId]);

  const showOtherRun = useCallback(() => {
    if (!otherRun) return;
    runGenRef.current += 1;
    setError(null);
    setStopping(false);
    setFinishRequested(false);
    followRun(otherRun);
  }, [otherRun, followRun]);

  const resetDiscovery = useCallback(() => {
    if (suiteId) dismissRun(suiteId);
    if (otherRun) {
      showOtherRun();
      return;
    }
    runGenRef.current += 1;
    setSuiteId(null);
    setSuite(null);
    setError(null);
    setStopping(false);
    setFinishRequested(false);
    setRunning(false);
  }, [suiteId, otherRun, showOtherRun]);

  const addPresetAsSet = useCallback(
    async (
      config: B4SetConfig,
      dscpPath?: DSCPPath,
      hosts?: string[],
    ): Promise<ApiResponse<AddPresetResult>> => {
      try {
        const res = await discoveryApi.addPresetAsSet(config, dscpPath, hosts);
        return { success: true, data: res };
      } catch (e) {
        return { success: false, error: describeApiError(e) };
      }
    },
    [],
  );

  const replaceStrategy = useCallback(
    async (
      setId: string,
      set: B4SetConfig,
      domains: string[],
      pins?: Record<string, string[]>,
      opts?: ReplaceStrategyOptions,
    ): Promise<ApiResponse<ReplaceStrategyResult>> => {
      try {
        const res = await discoveryApi.replaceStrategy(
          setId,
          set,
          domains,
          pins,
          opts,
        );
        return { success: true, data: res };
      } catch (e) {
        return { success: false, error: describeApiError(e) };
      }
    },
    [],
  );

  const markApplied = useCallback(
    async (domains: string[], preset: string, setId?: string) => {
      try {
        await discoveryApi.markApplied(domains, preset, setId);
        await loadHistory();
      } catch {
        return;
      }
    },
    [loadHistory],
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
    finishRequested,
    suiteId,
    suite,
    otherRun,
    error,
    history,
    historyLoading,
    startDiscovery,
    openRun,
    showOtherRun,
    cancelDiscovery,
    finishDiscovery,
    resetDiscovery,
    addPresetAsSet,
    replaceStrategy,
    markApplied,
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
  const { hasSession } = useAuth();

  useEffect(() => {
    if (!hasSession) return;
    let ws: WebSocket | null = null;
    let reconnectTimeout: ReturnType<typeof setTimeout> | null = null;
    let isCleaningUp = false;

    const connect = () => {
      if (isCleaningUp) return;

      const url = wsUrl("/api/ws/discovery");
      let opened = false;
      ws = new WebSocket(url);
      wsRef.current = ws;

      ws.onopen = () => {
        opened = true;
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
          if (!opened) verifySession();
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
  }, [hasSession]);

  const clearLogs = useCallback(() => {
    logsRef.current = [];
    setLogs([]);
  }, []);

  return { logs, connected, clearLogs };
}
