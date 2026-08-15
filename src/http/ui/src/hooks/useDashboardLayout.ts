import { useCallback, useEffect, useMemo, useState } from "react";
import {
  DASHBOARD_PANELS,
  GRID_COLUMNS,
  MIN_SPAN,
  PANELS_BY_ID,
} from "@components/dashboard/registry";

const STORAGE_KEY = "b4_dashboard_layout";
const LAYOUT_VERSION = 2;

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

const loadLayout = (): StoredLayout => {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return emptyLayout();
    const parsed = JSON.parse(raw) as Partial<StoredLayout>;
    if (parsed?.v !== LAYOUT_VERSION) return emptyLayout();
    const spans: Record<string, number> = {};
    for (const [id, span] of Object.entries(parsed.spans ?? {})) {
      if (PANELS_BY_ID.has(id) && Number.isFinite(span)) {
        spans[id] = clampSpan(span);
      }
    }
    return {
      v: LAYOUT_VERSION,
      order: mergeOrder(Array.isArray(parsed.order) ? parsed.order : []),
      hidden: (Array.isArray(parsed.hidden) ? parsed.hidden : []).filter((id) =>
        PANELS_BY_ID.has(id),
      ),
      spans,
    };
  } catch {
    return emptyLayout();
  }
};

export function useDashboardLayout() {
  const [layout, setLayout] = useState<StoredLayout>(loadLayout);

  useEffect(() => {
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(layout));
    } catch {
      /* storage unavailable */
    }
  }, [layout]);

  const hidden = useMemo(() => new Set(layout.hidden), [layout.hidden]);

  const move = useCallback((activeId: string, overId: string) => {
    setLayout((prev) => {
      const from = prev.order.indexOf(activeId);
      const to = prev.order.indexOf(overId);
      if (from < 0 || to < 0 || from === to) return prev;
      const order = [...prev.order];
      order.splice(to, 0, order.splice(from, 1)[0]);
      return { ...prev, order };
    });
  }, []);

  const setSpan = useCallback((id: string, span: number) => {
    setLayout((prev) => {
      const next = clampSpan(span);
      if (prev.spans[id] === next) return prev;
      return { ...prev, spans: { ...prev.spans, [id]: next } };
    });
  }, []);

  const setHidden = useCallback((id: string, value: boolean) => {
    setLayout((prev) => {
      const next = prev.hidden.filter((entry) => entry !== id);
      if (value) next.push(id);
      return { ...prev, hidden: next };
    });
  }, []);

  const reset = useCallback(() => setLayout(emptyLayout()), []);

  const customized = useMemo(
    () =>
      layout.hidden.length > 0 ||
      Object.keys(layout.spans).length > 0 ||
      layout.order.join() !== defaultOrder().join(),
    [layout],
  );

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
