import { useState, useCallback, useEffect, useRef } from "react";
import { ApiError } from "@api/apiClient";
import { detectorApi } from "@api/detector";
import type {
  DetectorLists,
  DetectorOptions,
  DetectorSuite,
} from "@models/detector";

const FINISHED = ["complete", "failed", "canceled"];

export function useDetector() {
  const [running, setRunning] = useState(false);
  const [suiteId, setSuiteId] = useState<string | null>(null);
  const [suite, setSuite] = useState<DetectorSuite | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [history, setHistory] = useState<DetectorSuite[]>([]);
  const [lists, setLists] = useState<DetectorLists | null>(null);
  const [listsBusy, setListsBusy] = useState(false);
  const initRef = useRef(false);

  const loadHistory = useCallback(async () => {
    try {
      const entries = await detectorApi.history();
      setHistory(Array.isArray(entries) ? entries : []);
    } catch {
      setHistory([]);
    }
  }, []);

  const loadLists = useCallback(async () => {
    try {
      setLists(await detectorApi.lists());
    } catch {
      setLists(null);
    }
  }, []);

  useEffect(() => {
    if (initRef.current) return;
    initRef.current = true;
    void loadHistory();
    void loadLists();
    detectorApi
      .current()
      .then((s) => {
        if (!s) return;
        setSuite(s);
        setSuiteId(s.id);
        setRunning(!FINISHED.includes(s.status));
      })
      .catch(() => {});
  }, [loadHistory, loadLists]);

  useEffect(() => {
    if (!suiteId || !running) return;
    let active = true;
    const poll = async () => {
      try {
        const data = await detectorApi.status(suiteId);
        if (!active) return;
        setSuite(data);
        if (FINISHED.includes(data.status)) {
          setRunning(false);
          void loadHistory();
        }
      } catch (e) {
        if (!active) return;
        if (e instanceof ApiError && e.status === 404) {
          setRunning(false);
          void loadHistory();
          return;
        }
        setError(e instanceof Error ? e.message : "Unknown error");
        setRunning(false);
      }
    };
    void poll();
    const timer = setInterval(() => void poll(), 1500);
    return () => {
      active = false;
      clearInterval(timer);
    };
  }, [suiteId, running, loadHistory]);

  const start = useCallback(async (options: DetectorOptions) => {
    setError(null);
    setSuite(null);
    setRunning(true);
    try {
      const res = await detectorApi.start(options);
      setSuiteId(res.id);
    } catch (e) {
      setRunning(false);
      setError(e instanceof Error ? e.message : "Failed to start");
    }
  }, []);

  const cancel = useCallback(async () => {
    if (!suiteId) return;
    try {
      await detectorApi.cancel(suiteId);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to stop");
    }
  }, [suiteId]);

  const reset = useCallback(() => {
    setSuiteId(null);
    setSuite(null);
    setError(null);
    setRunning(false);
  }, []);

  const open = useCallback((entry: DetectorSuite) => {
    setSuite(entry);
    setSuiteId(entry.id);
    setRunning(false);
    setError(null);
  }, []);

  const clearHistory = useCallback(async () => {
    try {
      await detectorApi.clearHistory();
      setHistory([]);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to clear history");
    }
  }, []);

  const deleteHistoryEntry = useCallback(async (id: string) => {
    try {
      await detectorApi.deleteHistoryEntry(id);
      setHistory((prev) => prev.filter((e) => e.id !== id));
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to delete");
    }
  }, []);

  const updateLists = useCallback(async (): Promise<string | null> => {
    setListsBusy(true);
    try {
      setLists(await detectorApi.updateLists());
      return null;
    } catch (e) {
      return e instanceof Error ? e.message : "Failed to update lists";
    } finally {
      setListsBusy(false);
    }
  }, []);

  const resetLists = useCallback(async () => {
    setListsBusy(true);
    try {
      setLists(await detectorApi.resetLists());
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to reset lists");
    } finally {
      setListsBusy(false);
    }
  }, []);

  return {
    running,
    suiteId,
    suite,
    error,
    history,
    lists,
    listsBusy,
    start,
    cancel,
    reset,
    open,
    clearHistory,
    deleteHistoryEntry,
    updateLists,
    resetLists,
  };
}
