import { useSyncExternalStore } from "react";
import type { DiscoverySuite } from "@models/discovery";

const TERMINAL = new Set(["complete", "failed", "canceled"]);

interface WatchState {
  run: string | null;
  pageRun: string | null;
}

type FinishedListener = (suite: DiscoverySuite, shown: boolean) => void;

let state: WatchState = { run: null, pageRun: null };
const settled = new Set<string>();
const listeners = new Set<() => void>();
const finishedListeners = new Set<FinishedListener>();

const update = (next: Partial<WatchState>) => {
  state = { ...state, ...next };
  listeners.forEach((listener) => listener());
};

export function watchRun(id: string) {
  if (settled.has(id) || state.run === id) return;
  update({ run: id });
}

export function forgetRun(id: string) {
  if (state.run === id) update({ run: null });
}

export function observeRun(
  suite: DiscoverySuite,
  { shown = false }: { shown?: boolean } = {},
) {
  if (suite.source && suite.source !== "web") return;
  if (!TERMINAL.has(suite.status)) {
    watchRun(suite.id);
    return;
  }
  settled.add(suite.id);
  if (state.run !== suite.id) return;
  update({ run: null });
  if (suite.status === "canceled") return;
  finishedListeners.forEach((listener) => listener(suite, shown));
}

export function showOnPage(id: string | null): () => void {
  if (state.pageRun !== id) update({ pageRun: id });
  return () => {
    if (id !== null && state.pageRun === id) update({ pageRun: null });
  };
}

export function onRunFinished(listener: FinishedListener): () => void {
  finishedListeners.add(listener);
  return () => {
    finishedListeners.delete(listener);
  };
}

const subscribe = (listener: () => void) => {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
};

const getState = () => state;

export const useDiscoveryWatch = (): WatchState =>
  useSyncExternalStore(subscribe, getState);
