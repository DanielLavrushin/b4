import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { apiGet, apiPut } from "@api/apiClient";
import {
  DASHBOARD_PANELS,
  GRID_COLUMNS,
  MIN_SPAN,
  PANELS_BY_ID,
} from "@components/dashboard/registry";

const STORAGE_KEY = "b4_dashboard_layout";
const LAYOUT_VERSION = 3;
const LAYOUT_ENDPOINT = "/api/ui/dashboard";
const SAVE_DEBOUNCE_MS = 800;

interface StoredDashboard {
  order?: string[];
  hidden?: string[];
  spans?: Record<string, number>;
}

interface StoredLayout {
  v: number;
  order: string[];
  hidden: string[];
  spans: Record<string, number>;
}

const defaultOrder = (): string[] => DASHBOARD_PANELS.map((p) => p.id);

export const clampSpan = (value: number): number =>
  Math.min(GRID_COLUMNS, Math.max(MIN_SPAN, Math.round(value)));

const mergeOrder = (saved: string[]): string[] => {
  const result = saved.filter((id) => PANELS_BY_ID.has(id));
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

const emptyLayout = (): StoredLayout => ({
  v: LAYOUT_VERSION,
  order: defaultOrder(),
  hidden: [],
  spans: {},
});

const normalize = (raw: StoredDashboard): StoredLayout => {
  const spans: Record<string, number> = {};
  for (const [id, span] of Object.entries(raw.spans ?? {})) {
    const panel = PANELS_BY_ID.get(id);
    if (!panel || !Number.isFinite(span)) continue;
    const value = clampSpan(span);
    if (value !== panel.defaultSpan) spans[id] = value;
  }
  return {
    v: LAYOUT_VERSION,
    order: mergeOrder(Array.isArray(raw.order) ? raw.order : []),
    hidden: (Array.isArray(raw.hidden) ? raw.hidden : []).filter((id) =>
      PANELS_BY_ID.has(id),
    ),
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

const isCustomized = (layout: StoredLayout): boolean =>
  layout.hidden.length > 0 ||
  Object.keys(layout.spans).length > 0 ||
  layout.order.join() !== defaultOrder().join();

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

  const move = useCallback(
    (activeId: string, overId: string) => {
      mutate((prev) => {
        const from = prev.order.indexOf(activeId);
        const to = prev.order.indexOf(overId);
        if (from < 0 || to < 0 || from === to) return prev;
        const order = [...prev.order];
        order.splice(to, 0, order.splice(from, 1)[0]);
        return { ...prev, order };
      });
    },
    [mutate],
  );

  const setSpan = useCallback(
    (id: string, span: number) => {
      mutate((prev) => {
        const next = clampSpan(span);
        if (next === PANELS_BY_ID.get(id)?.defaultSpan) {
          if (!(id in prev.spans)) return prev;
          const spans = { ...prev.spans };
          delete spans[id];
          return { ...prev, spans };
        }
        if (prev.spans[id] === next) return prev;
        return { ...prev, spans: { ...prev.spans, [id]: next } };
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
    hidden,
    spans: layout.spans,
    move,
    setSpan,
    setHidden,
    reset,
    customized,
  };
}
