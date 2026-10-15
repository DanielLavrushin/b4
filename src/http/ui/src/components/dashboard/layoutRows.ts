import { GRID_COLUMNS, MIN_SPAN } from "./registry";

export const ROW_CAPACITY = Math.floor(GRID_COLUMNS / MIN_SPAN);

export type DropSide = "before" | "after";

export interface Arrangement {
  order: readonly string[];
  breaks: readonly string[];
  spans: Readonly<Record<string, number>>;
}

export interface ArrangeContext {
  visible: (id: string) => boolean;
  defaultSpan: (id: string) => number;
}

export interface RowCell {
  id: string;
  span: number;
}

export function splitRows(order: readonly string[], breaks: readonly string[]): string[][] {
  const starts = new Set(breaks);
  const rows: string[][] = [];
  for (const id of order) {
    const last = rows.at(-1);
    if (!last || starts.has(id)) rows.push([id]);
    else last.push(id);
  }
  return rows;
}

export function wrapRows(order: readonly string[], spanOf: (id: string) => number): string[][] {
  const rows: string[][] = [];
  let used = 0;
  for (const id of order) {
    const span = spanOf(id);
    const last = rows.at(-1);
    if (!last || used + span > GRID_COLUMNS || last.length >= ROW_CAPACITY) {
      rows.push([id]);
      used = span;
    } else {
      last.push(id);
      used += span;
    }
  }
  return rows;
}

export function joinRows(rows: readonly (readonly string[])[]): { order: string[]; breaks: string[] } {
  const kept = rows.filter((row) => row.length > 0);
  return { order: kept.flat(), breaks: kept.slice(1).map((row) => row[0]) };
}

export function fitSpans(weights: readonly number[], total = GRID_COLUMNS): number[] {
  const n = weights.length;
  if (n === 0) return [];
  const floor = Math.min(MIN_SPAN, Math.floor(total / n));
  const pinned = new Array<boolean>(n).fill(false);
  const shares = new Array<number>(n).fill(floor);
  for (let pass = 0; pass < n; pass++) {
    let free = total;
    let weight = 0;
    for (let i = 0; i < n; i++) {
      if (pinned[i]) free -= floor;
      else weight += Math.max(weights[i], 1);
    }
    let pinnedMore = false;
    for (let i = 0; i < n; i++) {
      if (pinned[i]) continue;
      shares[i] = (Math.max(weights[i], 1) / weight) * free;
      if (shares[i] < floor) {
        pinned[i] = true;
        shares[i] = floor;
        pinnedMore = true;
      }
    }
    if (!pinnedMore) break;
  }
  const spans = shares.map((share) => Math.floor(share));
  let rest = total - spans.reduce((sum, span) => sum + span, 0);
  const byRemainder = shares
    .map((share, i) => ({ i, remainder: pinned[i] ? -1 : share - Math.floor(share) }))
    .sort((a, b) => b.remainder - a.remainder || a.i - b.i);
  for (const { i } of byRemainder) {
    if (rest <= 0) break;
    spans[i]++;
    rest--;
  }
  return spans;
}

const weightOf = (spans: Readonly<Record<string, number>>, ctx: ArrangeContext, id: string): number =>
  spans[id] ?? ctx.defaultSpan(id);

function chunks(row: readonly string[], ctx: ArrangeContext): string[][] {
  const shown = row.filter(ctx.visible);
  const out: string[][] = [];
  for (let i = 0; i < shown.length; i += ROW_CAPACITY) out.push(shown.slice(i, i + ROW_CAPACITY));
  return out;
}

export function shownRows(a: Arrangement, ctx: ArrangeContext): RowCell[][] {
  const out: RowCell[][] = [];
  for (const row of splitRows(a.order, a.breaks)) {
    for (const chunk of chunks(row, ctx)) {
      const spans = fitSpans(chunk.map((id) => weightOf(a.spans, ctx, id)));
      out.push(chunk.map((id, i) => ({ id, span: spans[i] })));
    }
  }
  return out;
}

function settle(
  a: Arrangement,
  rows: string[][],
  ctx: ArrangeContext,
  touched: readonly (readonly string[])[],
  placed: Record<string, number> = {},
): Arrangement {
  const spans: Record<string, number> = { ...a.spans, ...placed };
  for (const row of touched) {
    const shown = row.filter(ctx.visible);
    const before = shown.reduce((sum, id) => sum + weightOf(spans, ctx, id), 0);
    for (const chunk of chunks(row, ctx)) {
      const fitted = fitSpans(chunk.map((id) => weightOf(spans, ctx, id)));
      chunk.forEach((id, i) => {
        spans[id] = fitted[i];
      });
    }
    const groups = Math.max(1, Math.ceil(shown.length / ROW_CAPACITY));
    const scale = before > 0 ? (GRID_COLUMNS * groups) / before : 1;
    for (const id of row) {
      if (ctx.visible(id)) continue;
      spans[id] = Math.min(GRID_COLUMNS, Math.max(MIN_SPAN, Math.round(weightOf(spans, ctx, id) * scale)));
    }
  }
  return { ...joinRows(rows), spans };
}

const copyRows = (a: Arrangement): string[][] => splitRows(a.order, a.breaks).map((row) => [...row]);

export function moveBeside(
  a: Arrangement,
  ctx: ArrangeContext,
  id: string,
  targetId: string,
  side: DropSide,
): Arrangement {
  if (id === targetId) return a;
  const rows = copyRows(a);
  const from = rows.find((row) => row.includes(id));
  const to = rows.find((row) => row.includes(targetId));
  if (!from || !to) return a;
  from.splice(from.indexOf(id), 1);
  const at = to.indexOf(targetId) + (side === "after" ? 1 : 0);
  if (from === to) {
    to.splice(at, 0, id);
    return { ...joinRows(rows), spans: a.spans };
  }
  const peers = to.filter(ctx.visible);
  if (peers.length >= ROW_CAPACITY) {
    rows.splice(rows.indexOf(to) + 1, 0, [id]);
    return settle(a, rows, ctx, [from], { [id]: GRID_COLUMNS });
  }
  to.splice(at, 0, id);
  const share = peers.length
    ? peers.reduce((sum, peer) => sum + weightOf(a.spans, ctx, peer), 0) / peers.length
    : GRID_COLUMNS;
  return settle(a, rows, ctx, [from, to], { [id]: share });
}

export function moveToNewRow(
  a: Arrangement,
  ctx: ArrangeContext,
  id: string,
  beforeId: string | null,
): Arrangement {
  const rows = copyRows(a);
  const from = rows.find((row) => row.includes(id));
  if (!from) return a;
  const anchor = beforeId === null ? null : (rows.find((row) => row.includes(beforeId)) ?? null);
  const next = rows[rows.indexOf(from) + 1] ?? null;
  if (from.filter(ctx.visible).length === 1 && (anchor === from || anchor === next)) return a;
  from.splice(from.indexOf(id), 1);
  rows.splice(anchor ? rows.indexOf(anchor) : rows.length, 0, [id]);
  return settle(a, rows, ctx, [from], { [id]: GRID_COLUMNS });
}

export function resizePair(a: Arrangement, ctx: ArrangeContext, leftId: string, delta: number): Arrangement {
  for (const row of splitRows(a.order, a.breaks)) {
    for (const chunk of chunks(row, ctx)) {
      const i = chunk.indexOf(leftId);
      if (i < 0) continue;
      if (i + 1 >= chunk.length) return a;
      const fitted = fitSpans(chunk.map((id) => weightOf(a.spans, ctx, id)));
      const d = clampDelta(fitted[i], fitted[i + 1], delta);
      fitted[i] += d;
      fitted[i + 1] -= d;
      const spans: Record<string, number> = { ...a.spans };
      chunk.forEach((id, j) => {
        spans[id] = fitted[j];
      });
      return { ...a, spans };
    }
  }
  return a;
}

export function clampDelta(left: number, right: number, delta: number): number {
  return Math.max(MIN_SPAN - left, Math.min(delta, right - MIN_SPAN));
}

export type RowBreak = "split" | "join" | null;

function shownRowAbove(rows: readonly (readonly string[])[], r: number, ctx: ArrangeContext): number {
  for (let i = r - 1; i >= 0; i--) {
    if (rows[i].some(ctx.visible)) return i;
  }
  return -1;
}

export function rowBreakOf(a: Arrangement, ctx: ArrangeContext, id: string): { kind: RowBreak; allowed: boolean } {
  const rows = splitRows(a.order, a.breaks);
  const r = rows.findIndex((row) => row.includes(id));
  if (r < 0) return { kind: null, allowed: false };
  const shown = rows[r].filter(ctx.visible);
  if (shown[0] !== id) return { kind: "split", allowed: true };
  const above = shownRowAbove(rows, r, ctx);
  if (above < 0) return { kind: null, allowed: false };
  const room = rows[above].filter(ctx.visible).length + shown.length <= ROW_CAPACITY;
  return { kind: "join", allowed: room };
}

export function toggleRowBreak(a: Arrangement, ctx: ArrangeContext, id: string): Arrangement {
  const { kind, allowed } = rowBreakOf(a, ctx, id);
  if (!kind || !allowed) return a;
  const rows = copyRows(a);
  const r = rows.findIndex((row) => row.includes(id));
  if (kind === "split") {
    const row = rows[r];
    const at = row.indexOf(id);
    const head = row.slice(0, at);
    const tail = row.slice(at);
    rows.splice(r, 1, head, tail);
    return settle(a, rows, ctx, [head, tail]);
  }
  const above = shownRowAbove(rows, r, ctx);
  const merged = [...rows.slice(above, r).flat(), ...rows[r]];
  rows.splice(above, r - above + 1, merged);
  return settle(a, rows, ctx, [merged]);
}
