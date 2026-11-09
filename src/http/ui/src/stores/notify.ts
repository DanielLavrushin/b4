import { useSyncExternalStore } from "react";
import { setTitleAttention } from "@hooks/usePageTitle";

const PREF_KEY = "b4_notify_popups";
const POPUP_ICON = "/favicon-96x96.png";

export type PopupState = "unavailable" | "off" | "on" | "blocked";

export interface AwayNotice {
  title: string;
  body?: string;
  tag?: string;
  onOpen?: () => void;
}

const listeners = new Set<() => void>();
const emit = () => listeners.forEach((listener) => listener());

const popupApi = (): typeof Notification | null =>
  globalThis.isSecureContext && typeof Notification !== "undefined"
    ? Notification
    : null;

const readPref = (): boolean => {
  try {
    return localStorage.getItem(PREF_KEY) === "on";
  } catch {
    return false;
  }
};

const writePref = (on: boolean) => {
  try {
    if (on) {
      localStorage.setItem(PREF_KEY, "on");
    } else {
      localStorage.removeItem(PREF_KEY);
    }
  } catch {
    return;
  }
};

export const popupState = (): PopupState => {
  const api = popupApi();
  if (!api) return "unavailable";
  if (api.permission === "denied") return "blocked";
  return api.permission === "granted" && readPref() ? "on" : "off";
};

export async function setPopups(on: boolean): Promise<PopupState> {
  const api = popupApi();
  if (!api) return "unavailable";
  let permission = api.permission;
  if (on && permission === "default") {
    try {
      permission = await api.requestPermission();
    } catch {
      permission = api.permission;
    }
  }
  writePref(on && permission === "granted");
  emit();
  return popupState();
}

const subscribe = (listener: () => void) => {
  listeners.add(listener);
  globalThis.addEventListener("focus", listener);
  return () => {
    listeners.delete(listener);
    globalThis.removeEventListener("focus", listener);
  };
};

export const usePopupState = (): PopupState =>
  useSyncExternalStore(subscribe, popupState);

export const userAway = (): boolean =>
  document.hidden || !document.hasFocus();

export function whenBack(fn: () => void): void {
  if (!userAway()) {
    fn();
    return;
  }
  const back = () => {
    if (userAway()) return;
    document.removeEventListener("visibilitychange", back);
    globalThis.removeEventListener("focus", back);
    fn();
  };
  document.addEventListener("visibilitychange", back);
  globalThis.addEventListener("focus", back);
}

let holding = false;

const holdTitle = (text: string) => {
  setTitleAttention(text);
  if (holding) return;
  holding = true;
  whenBack(() => {
    holding = false;
    setTitleAttention("");
  });
};

const showPopup = ({ title, body, tag, onOpen }: AwayNotice) => {
  const api = popupApi();
  if (!api || popupState() !== "on") return;
  try {
    const popup = new api(title, { body, tag, icon: POPUP_ICON });
    popup.onclick = () => {
      globalThis.focus();
      onOpen?.();
      popup.close();
    };
  } catch {
    return;
  }
};

export function notifyAway(notice: AwayNotice): void {
  if (!userAway()) return;
  holdTitle(notice.title);
  showPopup(notice);
}
