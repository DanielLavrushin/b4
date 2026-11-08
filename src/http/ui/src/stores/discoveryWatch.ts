import { useSyncExternalStore } from "react";
import type { DiscoverySuite } from "@models/discovery";

const TERMINAL = new Set(["complete", "failed", "canceled"]);

interface WatchState {
  run: string | null;
  pageOpen: boolean;
}

type FinishedListener = (suite: DiscoverySuite) => void;

let state: WatchState = { run: null, pageOpen: false };
let pages = 0;
const listeners = new Set<() => void>();
const finishedListeners = new Set<FinishedListener>();

const update = (next: Partial<WatchState>) => {
  state = { ...state, ...next };
  listeners.forEach((listener) => listener());
};

export function watchRun(id: string) {
  if (state.run !== id) update({ run: id });
}

export function forgetRun(id: string) {
  if (state.run === id) update({ run: null });
}

export function observeRun(suite: DiscoverySuite) {
  if (suite.source && suite.source !== "web") return;
  if (!TERMINAL.has(suite.status)) {
    watchRun(suite.id);
    return;
  }
  if (state.run !== suite.id) return;
  update({ run: null });
  if (suite.status === "canceled") return;
  finishedListeners.forEach((listener) => listener(suite));
}

export function attachDiscoveryPage(): () => void {
  pages += 1;
  update({ pageOpen: true });
  return () => {
    pages -= 1;
    update({ pageOpen: pages > 0 });
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
