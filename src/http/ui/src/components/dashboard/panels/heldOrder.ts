import { useMemo, useState, type FocusEvent } from "react";

interface HeldOrder<T> {
  keys: string[];
  byKey: Map<string, T>;
}

export function useHeldOrder<T extends { key: string }>(entries: readonly T[]) {
  const [held, setHeld] = useState<HeldOrder<T> | null>(null);

  const rows = useMemo(() => {
    if (!held) return entries;
    const live = new Map(entries.map((entry) => [entry.key, entry]));
    return held.keys
      .map((key) => live.get(key) ?? held.byKey.get(key))
      .filter((entry): entry is T => entry !== undefined);
  }, [entries, held]);

  const hold = () =>
    setHeld(
      (prev) =>
        prev ?? {
          keys: entries.map((entry) => entry.key),
          byKey: new Map(entries.map((entry) => [entry.key, entry])),
        },
    );
  const release = () => setHeld(null);
  const releaseOnLeave = (e: FocusEvent<HTMLElement>) => {
    if (!e.currentTarget.contains(e.relatedTarget)) release();
  };

  return {
    rows,
    holdProps: {
      onMouseEnter: hold,
      onMouseLeave: release,
      onFocus: hold,
      onBlur: releaseOnLeave,
    },
  };
}
