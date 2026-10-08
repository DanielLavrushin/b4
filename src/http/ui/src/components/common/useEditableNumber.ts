import { useEffect, useRef, useState } from "react";

const NUMERIC_PATTERN = /^-?\d*$/;

export interface EditableNumber {
  editing: boolean;
  text: string;
  start: () => void;
  cancel: () => void;
  setText: (text: string) => void;
  commit: () => void;
  commitQuiet: () => void;
  close: () => void;
}

export function useEditableNumber(
  value: number,
  commitValue: (next: number) => void,
  min?: number,
  max?: number,
): EditableNumber {
  const [editing, setEditing] = useState(false);
  const [text, setText] = useState(() => String(value));
  const focusedRef = useRef(false);

  useEffect(() => {
    if (!focusedRef.current) setText(String(value));
  }, [value]);

  const clamp = (n: number): number => {
    if (min != null) n = Math.max(min, n);
    if (max != null) n = Math.min(max, n);
    return n;
  };

  return {
    editing,
    text,
    start: () => {
      focusedRef.current = true;
      setText(String(value));
      setEditing(true);
    },
    cancel: () => {
      focusedRef.current = false;
      setText(String(value));
      setEditing(false);
    },
    setText: (next: string) => {
      if (!NUMERIC_PATTERN.test(next)) return;
      setText(next);
      if (next === "" || next === "-") return;
      const n = Number(next);
      if (Number.isNaN(n)) return;
      const clamped = clamp(n);
      if (clamped !== value) commitValue(clamped);
    },
    commit: () => {
      focusedRef.current = false;
      setEditing(false);
      if (text === "" || text === "-") {
        setText(String(value));
        return;
      }
      const n = Number(text);
      if (Number.isNaN(n)) {
        setText(String(value));
        return;
      }
      const clamped = clamp(n);
      setText(String(clamped));
      if (clamped !== value) commitValue(clamped);
    },
    commitQuiet: () => {
      if (text === "" || text === "-") return;
      const n = Number(text);
      if (Number.isNaN(n)) return;
      const clamped = clamp(n);
      setText(String(clamped));
      if (clamped !== value) commitValue(clamped);
    },
    close: () => {
      focusedRef.current = false;
      setEditing(false);
    },
  };
}
