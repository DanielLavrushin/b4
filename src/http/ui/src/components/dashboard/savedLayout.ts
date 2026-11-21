import {
  GRID_COLUMNS,
  MIN_SPAN,
  joinRows,
  splitRows,
  wrapRows,
} from "./layoutRows";

export const LAYOUT_VERSION = 3;

export interface PanelSlot {
  readonly id: string;
  readonly defaultSpan: number;
}

export interface StoredDashboard {
  order?: string[];
  hidden?: string[];
  spans?: Record<string, number>;
  breaks?: string[];
}

export interface StoredLayout {
  v: number;
  order: string[];
  hidden: string[];
  spans: Record<string, number>;
  breaks: string[];
}

const clampSpan = (value: number): number =>
  Math.min(GRID_COLUMNS, Math.max(MIN_SPAN, Math.round(value)));

export const savedLayouts = (panels: readonly PanelSlot[]) => {
  const byId = new Map(panels.map((p) => [p.id, p]));

  const defaultSpanOf = (id: string): number => byId.get(id)?.defaultSpan ?? GRID_COLUMNS;

  const defaultRows = (): string[][] =>
    wrapRows(
      panels.map((p) => p.id),
      defaultSpanOf,
    );

  const mergeOrder = (saved: string[]): string[] => {
    const result = [...saved];
    panels.forEach((panel, index) => {
      if (result.includes(panel.id)) return;
      let insertAt = 0;
      for (let i = index - 1; i >= 0; i--) {
        const pos = result.indexOf(panels[i].id);
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
    panels.forEach((panel, index) => {
      if (out.some((row) => row.includes(panel.id))) return;
      let at = 0;
      for (let i = index - 1; i >= 0; i--) {
        const r = out.findIndex((row) => row.includes(panels[i].id));
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
      const panel = byId.get(id);
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

  const isOldDefault = (saved: string[], breaks: string[]): boolean => {
    const then = joinRows(
      wrapRows(
        panels.map((p) => p.id).filter((id) => saved.includes(id)),
        defaultSpanOf,
      ),
    );
    return then.order.join() === saved.join() && then.breaks.join() === breaks.join();
  };

  const normalize = (raw: StoredDashboard): StoredLayout => {
    const spans = normalizeSpans(raw.spans);
    const saved = (Array.isArray(raw.order) ? raw.order : []).filter(
      (id, i, all) => byId.has(id) && all.indexOf(id) === i,
    );
    const breaks = (Array.isArray(raw.breaks) ? raw.breaks : []).filter((id) => saved.includes(id));
    let rows: string[][];
    if (saved.length === 0 || isOldDefault(saved, breaks)) rows = defaultRows();
    else if (breaks.length > 0) rows = withNewPanels(splitRows(saved, breaks));
    else rows = wrapRows(mergeOrder(saved), (id) => spans[id] ?? defaultSpanOf(id));
    return {
      v: LAYOUT_VERSION,
      ...joinRows(rows),
      hidden: (Array.isArray(raw.hidden) ? raw.hidden : []).filter((id) => byId.has(id)),
      spans,
    };
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

  return { defaultSpanOf, normalizeSpans, emptyLayout, normalize, isCustomized };
};
