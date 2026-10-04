import { useRef, useSyncExternalStore } from "react";
import { wsUrl } from "@utils";
import { getAuthToken } from "@context/AuthProvider";
import type { MetricsFrame } from "@models/metrics";
import {
  MetricsStore,
  type FetchResult,
  type MetricsEnv,
  type MetricsLink,
  type MetricsSnapshot,
} from "./metricsStore";

export type {
  MetricsLink,
  MetricsLinkState,
  MetricsSnapshot,
} from "./metricsStore";

const METRICS_SOCKET_PATH = "/api/ws/metrics";

const browserEnv: MetricsEnv = {
  now: () => Date.now(),
  after: (ms, fn) => {
    const id = setTimeout(fn, ms);
    return () => clearTimeout(id);
  },
  random: () => Math.random(),
  openSocket: (handlers) => {
    const ws = new WebSocket(wsUrl(METRICS_SOCKET_PATH));
    ws.onopen = () => handlers.open();
    ws.onmessage = (event: MessageEvent<unknown>) => handlers.message(event.data);
    ws.onclose = () => handlers.close();
    return {
      close: () => {
        ws.onopen = null;
        ws.onmessage = null;
        ws.onerror = null;
        ws.onclose = null;
        try {
          ws.close();
        } catch {
          return;
        }
      },
    };
  },
  fetchJson: async (url, signal): Promise<FetchResult> => {
    const response = await fetch(url, {
      signal,
      cache: "no-store",
      headers: { Accept: "application/json" },
    });
    let body: unknown = null;
    try {
      body = await response.json();
    } catch {
      body = null;
    }
    return { status: response.status, body };
  },
  isHidden: () => typeof document !== "undefined" && document.hidden,
  onVisible: (fn) => {
    const handler = () => {
      if (!document.hidden) fn();
    };
    document.addEventListener("visibilitychange", handler);
    return () => document.removeEventListener("visibilitychange", handler);
  },
  onOnline: (fn) => {
    window.addEventListener("online", fn);
    return () => window.removeEventListener("online", fn);
  },
  onTokenChange: (fn) => {
    let last = getAuthToken();
    const handler = () => {
      const current = getAuthToken();
      if (current === last) return;
      last = current;
      fn();
    };
    window.addEventListener("storage", handler);
    return () => window.removeEventListener("storage", handler);
  },
};

export const metricsStore = new MetricsStore(browserEnv);

export const retryMetrics = (): void => metricsStore.retryNow();

export function serverNow(): number {
  return Date.now() + metricsStore.peekOffset();
}

interface SelectionMemo<T> {
  snapshot: MetricsSnapshot;
  selector: (snapshot: MetricsSnapshot) => T;
  value: T;
}

export function useMetrics<T>(
  selector: (snapshot: MetricsSnapshot) => T,
  isEqual: (a: T, b: T) => boolean = Object.is,
): T {
  const memo = useRef<SelectionMemo<T> | null>(null);
  const getSelection = (): T => {
    const snapshot = metricsStore.getSnapshot();
    const previous = memo.current;
    if (previous && previous.snapshot === snapshot && previous.selector === selector) {
      return previous.value;
    }
    const value = selector(snapshot);
    if (previous && isEqual(previous.value, value)) {
      memo.current = { snapshot, selector, value: previous.value };
      return previous.value;
    }
    memo.current = { snapshot, selector, value };
    return value;
  };
  return useSyncExternalStore(metricsStore.subscribe, getSelection);
}

export function useMetricsFrame<T>(
  selector: (frame: MetricsFrame) => T,
  isEqual: (a: T, b: T) => boolean = Object.is,
): T | undefined {
  return useMetrics<T | undefined>(
    (snapshot) => (snapshot.frame ? selector(snapshot.frame) : undefined),
    (a, b) => (a === undefined || b === undefined ? a === b : isEqual(a, b)),
  );
}

export function useMetricsLink(): MetricsLink {
  return useMetrics((snapshot) => snapshot.link);
}

export function useMetricsStale(): boolean {
  return useMetrics((snapshot) => snapshot.stale);
}

export interface MetricsStatus {
  link: MetricsLink;
  stale: boolean;
  hasFrame: boolean;
  receivedAt: number;
}

export function useMetricsStatus(): MetricsStatus {
  return useMetrics(
    (snapshot): MetricsStatus => ({
      link: snapshot.link,
      stale: snapshot.stale,
      hasFrame: snapshot.frame !== null,
      receivedAt: snapshot.receivedAt,
    }),
    shallowEqual,
  );
}

export function shallowEqual<T>(a: T, b: T): boolean {
  if (Object.is(a, b)) return true;
  if (typeof a !== "object" || typeof b !== "object" || a === null || b === null) {
    return false;
  }
  if (Array.isArray(a) !== Array.isArray(b)) return false;
  const ka = Object.keys(a);
  if (ka.length !== Object.keys(b).length) return false;
  for (const key of ka) {
    if (
      !Object.prototype.hasOwnProperty.call(b, key) ||
      !Object.is((a as Record<string, unknown>)[key], (b as Record<string, unknown>)[key])
    ) {
      return false;
    }
  }
  return true;
}

class Clock {
  private value: number;
  private timer: ReturnType<typeof setInterval> | null = null;
  private readonly listeners = new Set<() => void>();
  private readonly intervalMs: number;
  private readonly read: () => number;
  private offVisible: (() => void) | null = null;

  constructor(intervalMs: number, read: () => number) {
    this.intervalMs = intervalMs;
    this.read = read;
    this.value = this.quantized();
  }

  private quantized(): number {
    return Math.floor(this.read() / this.intervalMs) * this.intervalMs;
  }

  private readonly tick = () => {
    const next = this.quantized();
    if (next === this.value) return;
    this.value = next;
    if (typeof document !== "undefined" && document.hidden) return;
    for (const listener of [...this.listeners]) listener();
  };

  readonly subscribe = (listener: () => void) => {
    this.listeners.add(listener);
    if (this.listeners.size === 1) {
      this.timer = setInterval(this.tick, Math.min(1000, this.intervalMs));
      const onVisibility = () => {
        if (!document.hidden) this.tick();
      };
      document.addEventListener("visibilitychange", onVisibility);
      this.offVisible = () => document.removeEventListener("visibilitychange", onVisibility);
      this.tick();
    }
    return () => {
      this.listeners.delete(listener);
      if (this.listeners.size === 0) {
        if (this.timer !== null) clearInterval(this.timer);
        this.timer = null;
        this.offVisible?.();
        this.offVisible = null;
      }
    };
  };

  readonly get = (): number => {
    if (this.timer === null) this.value = this.quantized();
    return this.value;
  };
}

const clocks = new Map<string, Clock>();

function clockFor(intervalMs: number, server: boolean): Clock {
  const interval = Math.max(250, Math.round(intervalMs));
  const key = `${server ? "s" : "c"}${interval}`;
  let clock = clocks.get(key);
  if (!clock) {
    clock = new Clock(interval, server ? serverNow : Date.now);
    clocks.set(key, clock);
  }
  return clock;
}

export function useServerNow(intervalMs = 1000): number {
  const clock = clockFor(intervalMs, true);
  return useSyncExternalStore(clock.subscribe, clock.get);
}

export function useClientNow(intervalMs = 1000): number {
  const clock = clockFor(intervalMs, false);
  return useSyncExternalStore(clock.subscribe, clock.get);
}
