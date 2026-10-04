export const CHART_PADDING = 12;
export const AXIS_FONT_PX = 10;
export const X_AXIS_BAND = 18;
export const LABEL_GAP = 6;
export const BAR_MAX_WIDTH = 24;
export const BAR_GAP = 2;
export const BAR_GAP_NARROW = 1;
export const NARROW_SLOT_PX = 5;
export const MIN_SLOT_PX = 3;
export const MIN_SEGMENT_PX = 2;
export const SEGMENT_GAP_PX = 2;
export const MAX_RADIUS_PX = 2;
export const MIN_TICK_SPACING_PX = 80;
export const AXIS_LABEL_SPACING_PX = 8;
export const START_MATCH_MS = 2000;

export interface PlotBox {
  left: number;
  top: number;
  width: number;
  height: number;
  right: number;
  baseline: number;
}

export function plotBox(width: number, height: number, gutter: number): PlotBox {
  const left = CHART_PADDING + Math.max(0, Math.ceil(gutter));
  const top = CHART_PADDING;
  const plotWidth = Math.max(0, Math.floor(width) - left - CHART_PADDING);
  const plotHeight = Math.max(
    0,
    Math.floor(height) - top - CHART_PADDING - X_AXIS_BAND - 1,
  );
  return {
    left,
    top,
    width: plotWidth,
    height: plotHeight,
    right: left + plotWidth,
    baseline: top + plotHeight,
  };
}

export function gutterWidth(labelWidths: readonly number[]): number {
  let widest = 0;
  for (const w of labelWidths) if (w > widest) widest = w;
  return Math.ceil(widest) + LABEL_GAP;
}

export function mergeFactor(plotWidth: number, slots: number): number {
  if (!(plotWidth > 0) || !(slots > 0)) return 1;
  let factor = 1;
  while (factor < slots && plotWidth / Math.ceil(slots / factor) < MIN_SLOT_PX) {
    factor++;
  }
  return factor;
}

export interface BarColumn {
  slot: number;
  slotX: number;
  slotW: number;
  x: number;
  w: number;
  first: number;
  last: number;
  open: boolean;
}

export interface ColumnLayout {
  factor: number;
  slots: number;
  slotPx: number;
  plotWidth: number;
  bucketMs: number;
  newestGroup: number;
  from: number;
  to: number;
  columns: BarColumn[];
  bySlot: (BarColumn | undefined)[];
}

function makeColumn(slot: number, index: number, slotPx: number): BarColumn {
  const slotX = Math.round(slot * slotPx);
  const slotW = Math.round((slot + 1) * slotPx) - slotX;
  const gap = slotPx < NARROW_SLOT_PX ? BAR_GAP_NARROW : BAR_GAP;
  const inner = slotW - gap;
  const w = Math.max(1, Math.min(BAR_MAX_WIDTH, inner));
  const x = slotX + Math.max(0, Math.floor((inner - w) / 2));
  return { slot, slotX, slotW, x, w, first: index, last: index, open: false };
}

export function layoutColumns(
  times: readonly number[],
  slots: number,
  bucketMs: number,
  plotWidth: number,
  now: number,
): ColumnLayout {
  const factor = mergeFactor(plotWidth, slots);
  const merged = Math.max(1, Math.ceil(slots / factor));
  const slotPx = plotWidth > 0 ? plotWidth / merged : 0;
  const count = times.length;
  const newestAbs = Math.floor((count > 0 ? times[count - 1] : now) / bucketMs);
  const newestGroup = Math.floor(newestAbs / factor);
  const columns: BarColumn[] = [];
  const bySlot: (BarColumn | undefined)[] = new Array<BarColumn | undefined>(merged);
  for (let k = 0; k < count; k++) {
    const group = Math.floor((newestAbs - (count - 1 - k)) / factor);
    const slot = merged - 1 - (newestGroup - group);
    if (slot < 0) continue;
    const previous = columns[columns.length - 1];
    if (previous && previous.slot === slot) {
      previous.last = k;
      continue;
    }
    const column = makeColumn(slot, k, slotPx);
    columns.push(column);
    bySlot[slot] = column;
  }
  const newest = columns[columns.length - 1];
  if (newest && newest.last === count - 1) newest.open = true;
  return {
    factor,
    slots: merged,
    slotPx,
    plotWidth,
    bucketMs,
    newestGroup,
    from: (newestGroup - merged + 1) * factor * bucketMs,
    to: (newestGroup + 1) * factor * bucketMs,
    columns,
    bySlot,
  };
}

export function xOfTime(layout: ColumnLayout, time: number): number {
  const group = time / layout.bucketMs / layout.factor;
  return (layout.slots - 1 - (layout.newestGroup - group)) * layout.slotPx;
}

export function columnAt(layout: ColumnLayout, x: number): BarColumn | undefined {
  if (!(layout.slotPx > 0) || x < 0 || x >= layout.plotWidth) return undefined;
  return layout.bySlot[Math.min(layout.slots - 1, Math.floor(x / layout.slotPx))];
}

export function startMarkerX(
  layout: ColumnLayout,
  times: readonly number[],
  startedAt: number,
): number | null {
  if (!(startedAt > 0) || times.length === 0) return null;
  if (startedAt < layout.from || startedAt >= layout.to) return null;
  const first = layout.columns[0];
  if (first && Math.abs(times[first.first] - startedAt) <= START_MATCH_MS) {
    return first.slotX;
  }
  const x = Math.round(xOfTime(layout, startedAt));
  return x >= 0 && x <= layout.plotWidth ? x : null;
}

export const START_LABEL_GAP = 4;

export interface StartLabelPlacement {
  x: number;
  anchor: "start" | "end";
}

export function placeStartLabel(
  markerX: number,
  labelWidth: number,
  plotWidth: number,
): StartLabelPlacement | null {
  if (markerX + START_LABEL_GAP + labelWidth <= plotWidth) {
    return { x: markerX + START_LABEL_GAP, anchor: "start" };
  }
  if (markerX - START_LABEL_GAP - labelWidth >= 0) {
    return { x: markerX - START_LABEL_GAP, anchor: "end" };
  }
  return null;
}

export interface Segment {
  y: number;
  h: number;
}

export interface Stack {
  segments: (Segment | null)[];
  top: number | null;
}

export function stackSegments(
  values: readonly number[],
  max: number,
  plotHeight: number,
): Stack {
  const segments: (Segment | null)[] = [];
  let cursor = plotHeight;
  let cumulative = 0;
  let drawn = false;
  let top: number | null = null;
  for (const value of values) {
    if (!(value > 0) || !(max > 0) || plotHeight <= 0) {
      segments.push(null);
      continue;
    }
    cumulative += value;
    const target = plotHeight - Math.round((cumulative / max) * plotHeight);
    const start = drawn ? cursor - SEGMENT_GAP_PX : cursor;
    const segmentTop = Math.max(0, Math.min(target, start - MIN_SEGMENT_PX));
    const height = start - segmentTop;
    if (height <= 0) {
      segments.push(null);
      continue;
    }
    segments.push({ y: segmentTop, h: height });
    cursor = segmentTop;
    drawn = true;
    top = segmentTop;
  }
  return { segments, top };
}

export function barRadius(width: number): number {
  return Math.min(MAX_RADIUS_PX, width / 2);
}

export function topRoundedPath(
  x: number,
  y: number,
  w: number,
  h: number,
  radius: number,
): string {
  const r = Math.max(0, Math.min(radius, h, w / 2));
  if (r === 0) return `M${x},${y + h}V${y}H${x + w}V${y + h}Z`;
  return `M${x},${y + h}V${y + r}A${r},${r} 0 0 1 ${x + r},${y}H${x + w - r}A${r},${r} 0 0 1 ${x + w},${y + r}V${y + h}Z`;
}

export function valueRow(value: number, max: number, plotHeight: number): number {
  if (!(max > 0)) return plotHeight;
  return plotHeight - Math.round((value / max) * plotHeight);
}

export function tickStepMinutes(windowMs: number, plotWidth: number): number {
  const options = windowMs <= 3 * 3_600_000 ? [15, 30, 60] : [360, 720, 1440];
  for (const step of options) {
    if ((step * 60_000 * plotWidth) / windowMs >= MIN_TICK_SPACING_PX) return step;
  }
  return options[options.length - 1];
}

export function localTicks(from: number, to: number, stepMinutes: number): number[] {
  if (!(to > from) || !(stepMinutes > 0)) return [];
  const cursor = new Date(from);
  cursor.setSeconds(0, 0);
  const minuteOfDay = cursor.getHours() * 60 + cursor.getMinutes();
  cursor.setHours(0, Math.ceil(minuteOfDay / stepMinutes) * stepMinutes, 0, 0);
  let guard = 0;
  while (cursor.getTime() < from && guard++ < 1000) {
    cursor.setMinutes(cursor.getMinutes() + stepMinutes);
  }
  const ticks: number[] = [];
  while (cursor.getTime() <= to && guard++ < 2000) {
    ticks.push(cursor.getTime());
    cursor.setMinutes(cursor.getMinutes() + stepMinutes);
  }
  return ticks;
}

export interface AxisLabel {
  t: number;
  x: number;
}

export function placeXLabels(
  ticks: readonly number[],
  layout: ColumnLayout,
  widthOf: (t: number) => number,
  minX: number,
  maxX: number,
): AxisLabel[] {
  const labels: AxisLabel[] = [];
  let lastRight = Number.NEGATIVE_INFINITY;
  for (const t of ticks) {
    const x = Math.round(xOfTime(layout, t));
    const half = widthOf(t) / 2;
    if (x - half < minX || x + half > maxX) continue;
    if (x - half < lastRight + AXIS_LABEL_SPACING_PX) continue;
    labels.push({ t, x });
    lastRight = x + half;
  }
  return labels;
}

export interface SeriesSummary {
  totals: number[];
  busiest: number;
  busiestTotal: number;
  allZero: boolean;
}

export function summarizeRows(rows: readonly (readonly number[])[]): SeriesSummary {
  const totals: number[] = [];
  let busiest = -1;
  let busiestTotal = 0;
  for (let i = 0; i < rows.length; i++) {
    const row = rows[i];
    let sum = 0;
    for (let s = 0; s < row.length; s++) {
      const v = row[s] > 0 ? row[s] : 0;
      totals[s] = (totals[s] ?? 0) + v;
      sum += v;
    }
    if (sum > busiestTotal) {
      busiestTotal = sum;
      busiest = i;
    }
  }
  return { totals, busiest, busiestTotal, allZero: busiest < 0 };
}

export interface SparkInput {
  times: readonly number[];
  values: readonly number[];
  now: number;
  windowMs: number;
  bucketMs: number;
  width: number;
  height: number;
  ceiling: number;
  inset?: number;
}

const round2 = (v: number): number => Math.round(v * 100) / 100;

export interface SparkCoord {
  x: number;
  y: number;
  index: number;
}

export function sparkCoords(input: SparkInput): SparkCoord[] | null {
  const inset = input.inset ?? 1;
  const from = input.now - input.windowMs;
  const spanX = Math.max(0, input.width - 2 * inset);
  const spanY = Math.max(0, input.height - 2 * inset);
  if (!(input.windowMs > 0) || !(input.ceiling > 0)) return null;
  const coords: SparkCoord[] = [];
  for (let i = 0; i < input.times.length; i++) {
    const center = input.times[i] + input.bucketMs / 2;
    if (center < from) continue;
    const t = Math.min(center, input.now);
    const v = Math.max(0, Math.min(input.values[i], input.ceiling));
    coords.push({
      x: round2(inset + ((t - from) / input.windowMs) * spanX),
      y: round2(inset + spanY - (v / input.ceiling) * spanY),
      index: i,
    });
  }
  return coords.length >= 2 ? coords : null;
}

export function sparkPoints(input: SparkInput): string | null {
  const coords = sparkCoords(input);
  return coords ? coords.map((c) => `${c.x},${c.y}`).join(" ") : null;
}

export function nearestCoord(coords: readonly SparkCoord[], x: number): number {
  let best = 0;
  let bestDistance = Number.POSITIVE_INFINITY;
  for (let i = 0; i < coords.length; i++) {
    const distance = Math.abs(coords[i].x - x);
    if (distance < bestDistance) {
      bestDistance = distance;
      best = i;
    }
  }
  return best;
}
