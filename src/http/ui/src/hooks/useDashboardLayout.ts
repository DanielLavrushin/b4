import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { apiGet, apiPut } from "@api/apiClient";
import {
  DASHBOARD_PANELS,
  GRID_COLUMNS,
  MIN_SPAN,
  PANELS_BY_ID,
} from "@components/dashboard/registry";
import {
  joinRows,
  splitRows,
  wrapRows,
  type Arrangement,
} from "@components/dashboard/layoutRows";

const STORAGE_KEY = "b4_dashboard_layout";
const LAYOUT_VERSION = 3;
const LAYOUT_ENDPOINT = "/api/ui/dashboard";
const SAVE_DEBOUNCE_MS = 800;

interface StoredDashboard {
  order?: string[];
  hidden?: string[];
  spans?: Record<string, number>;
  breaks?: string[];
}

interface StoredLayout {
  v: number;
  order: string[];
  hidden: string[];
  spans: Record<string, number>;
  breaks: string[];
}

export const defaultSpanOf = (id: string): number =>
  PANELS_BY_ID.get(id)?.defaultSpan ?? GRID_COLUMNS;

const clampSpan = (value: number): number =>
  Math.min(GRID_COLUMNS, Math.max(MIN_SPAN, Math.round(value)));

const defaultRows = (): string[][] =>
  wrapRows(
    DASHBOARD_PANELS.map((p) => p.id),
    defaultSpanOf,
  );

const mergeOrder = (saved: string[]): string[] => {
  const result = [...saved];
  DASHBOARD_PANELS.forEach((panel, index) => {
    if (result.includes(panel.id)) return;
    let insertAt = 0;
    for (let i = index - 1; i >= 0; i--) {
      const pos = result.indexOf(DASHBOARD_PANELS[i].id);
      if (pos >= 0) {
        insertAt = pos + 1;
        break;
      }
    }
    result.splice(insertAt, 0, panel.id);
  });
  return result;
};

const withNewPanels = (rows: string[][]): string[][] => {
  const out = rows.map((row) => [...row]);
  DASHBOARD_PANELS.forEach((panel, index) => {
    if (out.some((row) => row.includes(panel.id))) return;
    let at = 0;
    for (let i = index - 1; i >= 0; i--) {
      const r = out.findIndex((row) => row.includes(DASHBOARD_PANELS[i].id));
      if (r >= 0) {
        at = r + 1;
        break;
      }
    }
    out.splice(at, 0, [panel.id]);
  });
  return out;
};

const normalizeSpans = (raw: Readonly<Record<string, number>> | undefined): Record<string, number> => {
  const spans: Record<string, number> = {};
  for (const [id, span] of Object.entries(raw ?? {})) {
    const panel = PANELS_BY_ID.get(id);
    if (!panel || !Number.isFinite(span)) continue;
    const value = clampSpan(span);
    if (value !== panel.defaultSpan) spans[id] = value;
  }
  return spans;
};

const emptyLayout = (): StoredLayout => ({
  v: LAYOUT_VERSION,
  ...joinRows(defaultRows()),
  hidden: [],
  spans: {},
});

const normalize = (raw: StoredDashboard): StoredLayout => {
  const spans = normalizeSpans(raw.spans);
  const saved = (Array.isArray(raw.order) ? raw.order : []).filter(
    (id, i, all) => PANELS_BY_ID.has(id) && all.indexOf(id) === i,
  );
  const breaks = (Array.isArray(raw.breaks) ? raw.breaks : []).filter((id) => saved.includes(id));
  let rows: string[][];
  if (saved.length === 0) rows = defaultRows();
  else if (breaks.length > 0) rows = withNewPanels(splitRows(saved, breaks));
  else rows = wrapRows(mergeOrder(saved), (id) => spans[id] ?? defaultSpanOf(id));
  return {
    v: LAYOUT_VERSION,
    ...joinRows(rows),
    hidden: (Array.isArray(raw.hidden) ? raw.hidden : []).filter((id) => PANELS_BY_ID.has(id)),
    spans,
  };
};

const loadLayout = (): StoredLayout => {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return emptyLayout();
    const parsed = JSON.parse(raw) as Partial<StoredLayout>;
    if (parsed?.v !== LAYOUT_VERSION) return emptyLayout();
    return normalize(parsed);
  } catch {
    return emptyLayout();
  }
};

const saveLocal = (layout: StoredLayout): void => {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(layout));
  } catch {
    return;
  }
};

const isCustomized = (layout: StoredLayout): boolean => {
  const defaults = emptyLayout();
  return (
    layout.hidden.length > 0 ||
    Object.keys(layout.spans).length > 0 ||
    layout.order.join() !== defaults.order.join() ||
    layout.breaks.join() !== defaults.breaks.join()
  );
};

export function useDashboardLayout() {
  const [layout, setLayout] = useState<StoredLayout>(loadLayout);
  const [hydrated, setHydrated] = useState(false);
  const dirty = useRef(false);

  useEffect(() => {
    let cancelled = false;
    apiGet<StoredDashboard>(LAYOUT_ENDPOINT)
      .then((remote) => {
        if (cancelled || dirty.current) return;
        const next = normalize(remote ?? {});
        if (isCustomized(next)) setLayout(next);
      })
      .catch(() => undefined)
      .finally(() => {
        if (!cancelled) setHydrated(true);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    saveLocal(layout);
    if (!hydrated || !dirty.current) return;
    const timer = setTimeout(() => {
      void apiPut<StoredDashboard>(LAYOUT_ENDPOINT, {
        order: layout.order,
        hidden: layout.hidden,
        spans: layout.spans,
        breaks: layout.breaks,
      }).catch(() => undefined);
    }, SAVE_DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [layout, hydrated]);

  const mutate = useCallback(
    (updater: (prev: StoredLayout) => StoredLayout) => {
      dirty.current = true;
      setLayout(updater);
    },
    [],
  );

  const hidden = useMemo(() => new Set(layout.hidden), [layout.hidden]);

  const arrange = useCallback(
    (change: (current: Arrangement) => Arrangement) => {
      mutate((prev) => {
        const next = change(prev);
        if (next === prev) return prev;
        return {
          ...prev,
          order: [...next.order],
          breaks: [...next.breaks],
          spans: normalizeSpans(next.spans),
        };
      });
    },
    [mutate],
  );

  const setHidden = useCallback(
    (id: string, value: boolean) => {
      mutate((prev) => {
        const next = prev.hidden.filter((entry) => entry !== id);
        if (value) next.push(id);
        return { ...prev, hidden: next };
      });
    },
    [mutate],
  );

  const reset = useCallback(() => mutate(emptyLayout), [mutate]);

  const customized = useMemo(() => isCustomized(layout), [layout]);

  return {
    order: layout.order,
    breaks: layout.breaks,
    hidden,
    spans: layout.spans,
    arrange,
    setHidden,
    reset,
    customized,
  };
}
