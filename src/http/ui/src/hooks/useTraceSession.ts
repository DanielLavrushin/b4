import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useSnackbar } from "@context/SnackbarProvider";
import { traceApi } from "@api/trace";

export interface TraceSession {
  tracing: boolean;
  traceBusy: boolean;
  traceLines: number;
  traceElapsed: number;
  downloadReady: boolean;
  startTrace: () => void;
  stopTrace: () => void;
  downloadTrace: () => void;
}

export function useTraceSession(): TraceSession {
  const { t } = useTranslation();
  const { showSuccess, showError } = useSnackbar();
  const [tracing, setTracing] = useState(false);
  const [traceBusy, setTraceBusy] = useState(false);
  const [traceLines, setTraceLines] = useState(0);
  const [traceElapsed, setTraceElapsed] = useState(0);
  const [traceStartMs, setTraceStartMs] = useState<number | null>(null);
  const [downloadReady, setDownloadReady] = useState(false);

  useEffect(() => {
    let cancelled = false;
    traceApi
      .status()
      .then((s) => {
        if (cancelled) return;
        setDownloadReady(s.downloadReady);
        if (s.active) {
          setTracing(true);
          setTraceLines(s.lines);
          setTraceStartMs(
            s.startedAt ? new Date(s.startedAt).getTime() : Date.now(),
          );
        }
      })
      .catch(() => undefined);
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    if (!tracing || traceStartMs == null) return;
    let cancelled = false;
    const tick = () =>
      setTraceElapsed(Math.floor((Date.now() - traceStartMs) / 1000));
    tick();
    const elapsedTimer = setInterval(tick, 1000);
    const pollTimer = setInterval(() => {
      traceApi
        .status()
        .then((s) => {
          if (cancelled) return;
          setTraceLines(s.lines);
          if (!s.active) {
            setTracing(false);
            setTraceStartMs(null);
            setDownloadReady(s.downloadReady);
          }
        })
        .catch(() => undefined);
    }, 2000);
    return () => {
      cancelled = true;
      clearInterval(elapsedTimer);
      clearInterval(pollTimer);
    };
  }, [tracing, traceStartMs]);

  const startTrace = () => {
    setTraceBusy(true);
    traceApi
      .start()
      .then((s) => {
        setTracing(true);
        setTraceLines(s.lines);
        setTraceElapsed(0);
        setTraceStartMs(
          s.startedAt ? new Date(s.startedAt).getTime() : Date.now(),
        );
        showSuccess(t("logs.trace.started"));
      })
      .catch(() => showError(t("logs.trace.startFailed")))
      .finally(() => setTraceBusy(false));
  };

  const stopTrace = () => {
    setTraceBusy(true);
    traceApi
      .stop()
      .then(async (s) => {
        setTracing(false);
        setTraceStartMs(null);
        setTraceLines(s.lines);
        setDownloadReady(true);
        try {
          await traceApi.download();
          showSuccess(t("logs.trace.saved"));
        } catch {
          showError(t("logs.trace.downloadFailed"));
        }
      })
      .catch(() => showError(t("logs.trace.stopFailed")))
      .finally(() => setTraceBusy(false));
  };

  const downloadTrace = () => {
    traceApi.download().catch(() => showError(t("logs.trace.downloadFailed")));
  };

  return {
    tracing,
    traceBusy,
    traceLines,
    traceElapsed,
    downloadReady,
    startTrace,
    stopTrace,
    downloadTrace,
  };
}
