import { useEffect, useRef } from "react";

export interface Hotkey {
  keys: string;
  labelKey: string;
  run: () => void;
}

const editable = (target: EventTarget | null): boolean => {
  const el = target as HTMLElement | null;
  if (!el) return false;
  const tag = el.tagName;
  return tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || el.isContentEditable;
};

const insideOverlay = (target: EventTarget | null): boolean => {
  const el = target instanceof Element ? target : null;
  if (el?.closest(".MuiModal-root, [role=dialog], [role=menu], [role=listbox]")) return true;
  return document.querySelector(".MuiModal-root:not(.MuiModal-hidden)") !== null;
};

const activatable = (target: EventTarget | null): boolean => {
  const el = target instanceof Element ? target : null;
  return el?.closest("button, a, [role=button], [role=link], [role=tab], [role=checkbox], [role=switch], [role=menuitem]") != null;
};

const keyName = (e: KeyboardEvent): string => {
  const letter = /^Key[A-Z]$/.test(e.code) ? e.code.slice(3).toLowerCase() : null;
  const key = letter ?? (e.key.length === 1 ? e.key.toLowerCase() : e.key);
  return e.shiftKey && (letter !== null || e.key.length === 1) && key !== "?" ? `shift+${key}` : key;
};

export function useHotkeys(bindings: Hotkey[], enabled: boolean) {
  const ref = useRef(bindings);
  const pending = useRef<{ key: string; at: number } | null>(null);

  useEffect(() => {
    ref.current = bindings;
  }, [bindings]);

  useEffect(() => {
    if (!enabled) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.defaultPrevented || e.isComposing || e.ctrlKey || e.metaKey || e.altKey || editable(e.target) || insideOverlay(e.target)) return;
      const key = keyName(e);
      if ((key === "Enter" || key === " ") && activatable(e.target)) return;
      const now = Date.now();
      const chord = pending.current && now - pending.current.at < 1000 ? `${pending.current.key} ${key}` : null;
      const match = (chord ? ref.current.find((b) => b.keys.split("|").includes(chord)) : undefined) ?? ref.current.find((b) => b.keys.split("|").includes(key));
      if (match) {
        e.preventDefault();
        pending.current = null;
        match.run();
        return;
      }
      pending.current = ref.current.some((b) => b.keys.split("|").some((k) => k.startsWith(key + " "))) ? { key, at: now } : null;
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [enabled]);
}
