import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { watchdogApi } from "@api/watchdog";
import { SetWatchStatus, WatchdogState } from "@models/watchdog";

export const watchdogKeys = {
  status: ["watchdog", "status"] as const,
};

export function useWatchdogStatus(enabled = true) {
  return useQuery({
    queryKey: watchdogKeys.status,
    queryFn: () => watchdogApi.status(),
    enabled,
    staleTime: 10 * 1000,
    refetchInterval: enabled ? 30 * 1000 : false,
    retry: false,
  });
}

export function useWatchdogSetStatuses(enabled = true) {
  const query = useWatchdogStatus(enabled);
  const data = query.data;
  const byId = useMemo(() => {
    const map = new Map<string, SetWatchStatus>();
    for (const s of data?.sets ?? []) map.set(s.set_id, s);
    return map;
  }, [data]);
  return { byId, enabled: data?.enabled ?? false, loaded: !!data };
}

export function useWatchdog() {
  const queryClient = useQueryClient();
  const [state, setState] = useState<WatchdogState | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const initRef = useRef(false);

  const loadStatus = useCallback(async () => {
    try {
      const data = await watchdogApi.status();
      setState(data);
      queryClient.setQueryData(watchdogKeys.status, data);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load watchdog status");
    } finally {
      setLoading(false);
    }
  }, [queryClient]);

  useEffect(() => {
    if (initRef.current) return;
    initRef.current = true;
    loadStatus().catch(() => {});
  }, [loadStatus]);

  useEffect(() => {
    const interval = setInterval(() => {
      loadStatus().catch(() => {});
    }, 5000);
    return () => clearInterval(interval);
  }, [loadStatus]);

  const forceCheck = useCallback(async (domain: string) => {
    try {
      await watchdogApi.forceCheck(domain);
      await loadStatus();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Force check failed");
    }
  }, [loadStatus]);

  const addDomain = useCallback(async (domain: string) => {
    try {
      await watchdogApi.addDomain(domain);
      await loadStatus();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to add domain");
      throw err;
    }
  }, [loadStatus]);

  const removeDomain = useCallback(async (domain: string) => {
    try {
      await watchdogApi.removeDomain(domain);
      await loadStatus();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to remove domain");
    }
  }, [loadStatus]);

  const toggleEnabled = useCallback(async (enabled: boolean) => {
    try {
      if (enabled) {
        await watchdogApi.enable();
      } else {
        await watchdogApi.disable();
      }
      await loadStatus();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to toggle watchdog");
    }
  }, [loadStatus]);

  const checkSet = useCallback(async (setId: string) => {
    const res = await watchdogApi.checkSet(setId);
    await loadStatus();
    return res?.outcome;
  }, [loadStatus]);

  const setSetEnabled = useCallback(async (setId: string, enabled: boolean) => {
    await watchdogApi.setEnabled(setId, enabled);
    await loadStatus();
  }, [loadStatus]);

  const moveDomain = useCallback(async (domain: string, setId: string) => {
    await watchdogApi.moveDomain(domain, setId);
    await loadStatus();
  }, [loadStatus]);

  return {
    state,
    loading,
    error,
    forceCheck,
    addDomain,
    removeDomain,
    toggleEnabled,
    checkSet,
    setSetEnabled,
    moveDomain,
    refresh: loadStatus,
  };
}
