import { useEffect, useSyncExternalStore } from "react";

const APP_NAME = "B4";

let detail = "";
const listeners = new Set<() => void>();

function setDetail(value: string) {
  detail = value;
  listeners.forEach((listener) => listener());
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

function getDetail() {
  return detail;
}

export function usePageTitleDetail(value: string | undefined) {
  useEffect(() => {
    if (!value) return;
    setDetail(value);
    return () => setDetail("");
  }, [value]);
}

export function useDocumentTitle(page: string) {
  const current = useSyncExternalStore(subscribe, getDetail);

  useEffect(() => {
    document.title = [current, page, APP_NAME].filter(Boolean).join(" - ");
  }, [current, page]);
}
