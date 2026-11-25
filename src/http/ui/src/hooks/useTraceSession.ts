import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useAuth } from "@context/AuthProvider";
import { useSnackbar } from "@context/SnackbarProvider";
import { ApiError } from "@api/apiClient";
import { traceApi, type TraceEndReason, type TraceStatus } from "@api/trace";

const POLL_MS = 2000;

export interface TraceSession {
  tracing: boolean;
  traceBusy: boolean;
  traceKnown: boolean;
  traceLines: number;
  traceElapsed: number;
  traceMaxMinutes: number;
  downloadReady: boolean;
  traceEndReason?: TraceEndReason;
  startTrace: () => void;
  stopTrace: () => void;
  downloadTrace: () => void;
}

export function useTraceSession(): TraceSession {
  const { t } = useTranslation();
  const { showSnackbar, showSuccess, showError } = useSnackbar();
  const { hasSession } = useAuth();
  const [status, setStatus] = useState<TraceStatus>();
  const [traceBusy, setTraceBusy] = useState(false);
  const [traceElapsed, setTraceElapsed] = useState(0);
  const requests = useRef({ sent: 0, applied: 0 });
  const wasTracing = useRef(false);
  const tracing = status?.active ?? false;
  const startedAt = status?.startedAt;

  const readStatus = useCallback(async () => {
    const order = requests.current;
    order.sent += 1;
    const mine = order.sent;
    const answer = await traceApi.status();
    if (mine > order.applied) {
      order.applied = mine;
      setStatus(answer);
    }
  }, []);

  const command = useCallback(async (send: () => Promise<TraceStatus>) => {
    const answer = await send();
    const order = requests.current;
    order.sent += 1;
    order.applied = order.sent;
    setStatus(answer);
    return answer;
  }, []);

  const refresh = useCallback(
    () => readStatus().catch(() => undefined),
    [readStatus],
  );

  useEffect(() => {
    if (!hasSession) return;
    let cancelled = false;
    let retry: ReturnType<typeof setTimeout> | undefined;
    const read = () => {
      readStatus().catch(() => {
        if (!cancelled) retry = setTimeout(read, POLL_MS);
      });
    };
    read();
    return () => {
      cancelled = true;
      clearTimeout(retry);
    };
  }, [hasSession, readStatus]);

  useEffect(() => {
    if (!tracing || traceBusy) return;
    const poll = setInterval(() => void refresh(), POLL_MS);
    return () => clearInterval(poll);
  }, [tracing, traceBusy, refresh]);

  useEffect(() => {
    if (!tracing || !startedAt) return;
    const started = new Date(startedAt).getTime();
    const tick = () =>
      setTraceElapsed(Math.floor((Date.now() - started) / 1000));
    tick();
    const timer = setInterval(tick, 1000);
    return () => clearInterval(timer);
  }, [tracing, startedAt]);

  useEffect(() => {
    if (!status) return;
    if (wasTracing.current && !status.active && !status.downloadReady) {
      showSnackbar(t("logs.trace.endedLost"));
    }
    wasTracing.current = status.active;
  }, [status, showSnackbar, t]);

  const startTrace = () => {
    setTraceBusy(true);
    command(() => traceApi.start())
      .then(() => showSuccess(t("logs.trace.started")))
      .catch((e: unknown) => {
        showError(
          e instanceof ApiError && e.status === 507
            ? t("logs.trace.noSpace")
            : t("logs.trace.startFailed"),
        );
        return refresh();
      })
      .finally(() => setTraceBusy(false));
  };

  const downloadTrace = () =>
    traceApi
      .download()
      .then(() => true)
      .catch(async () => {
        showError(t("logs.trace.downloadFailed"));
        await refresh();
        return false;
      });

  const stopTrace = () => {
    setTraceBusy(true);
    command(traceApi.stop)
      .then(async (s) => {
        if (s.downloadReady && (await downloadTrace())) {
          showSuccess(t("logs.trace.saved"));
        }
      })
      .catch((e: unknown) => {
        if (!(e instanceof ApiError && e.status === 400)) {
          showError(t("logs.trace.stopFailed"));
        }
        return refresh();
      })
      .finally(() => setTraceBusy(false));
  };

  return {
    tracing,
    traceBusy,
    traceKnown: status !== undefined,
    traceLines: status?.lines ?? 0,
    traceElapsed,
    traceMaxMinutes: Math.round((status?.maxSeconds ?? 0) / 60),
    downloadReady: status?.downloadReady ?? false,
    traceEndReason: status?.endReason,
    startTrace,
    stopTrace,
    downloadTrace: () => void downloadTrace(),
  };
}
