import { useEffect, useSyncExternalStore } from "react";

const APP_NAME = "B4";

let detail = "";
let attention = "";
const listeners = new Set<() => void>();

function setDetail(value: string) {
  detail = value;
  listeners.forEach((listener) => listener());
}

export function setTitleAttention(value: string) {
  if (attention === value) return;
  attention = value;
  listeners.forEach((listener) => listener());
}

function getAttention() {
  return attention;
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
  const notice = useSyncExternalStore(subscribe, getAttention);

  useEffect(() => {
    document.title = [notice && `● ${notice}`, current, page, APP_NAME]
      .filter(Boolean)
      .join(" - ");
  }, [notice, current, page]);
}
