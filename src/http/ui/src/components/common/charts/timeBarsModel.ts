import { formatClock, formatCount } from "./format";
import { nextScale, yTicks, type ScaleState } from "./scale";
import {
  CHART_PADDING,
  gutterWidth,
  layoutColumns,
  localTicks,
  placeXLabels,
  plotBox,
  stackSegments,
  startMarkerX,
  tickStepMinutes,
  valueRow,
  type AxisLabel,
  type BarColumn,
  type ColumnLayout,
  type PlotBox,
  type Stack,
} from "./geometry";

export interface TimeBarsModelInput {
  width: number;
  height: number;
  times: readonly number[];
  rows: readonly (readonly number[])[];
  slots: number;
  bucketMs: number;
  now: number;
  divisor: number;
  locale: string;
  startedAt?: number;
  prevScale: ScaleState | null;
  measure: (text: string) => number;
  nowLabel: string;
}

export interface ColumnModel {
  column: BarColumn;
  raw: number[];
  rawTotal: number;
  count: number;
  values: number[];
  stack: Stack;
  t0: number;
  t1: number;
  open: boolean;
}

export interface YTick {
  value: number;
  label: string;
  row: number;
}

export interface XLabel extends AxisLabel {
  label: string;
}

export interface TimeBarsModel {
  box: PlotBox;
  layout: ColumnLayout;
  columns: ColumnModel[];
  scale: ScaleState;
  yTicks: YTick[];
  gutter: number;
  empty: boolean;
  xLabels: XLabel[];
  startX: number | null;
}

interface ColumnValues {
  column: BarColumn;
  raw: number[];
  rawTotal: number;
  count: number;
  values: number[];
  t0: number;
  t1: number;
}

function columnValues(
  layout: ColumnLayout,
  input: TimeBarsModelInput,
  seriesCount: number,
): { items: ColumnValues[]; peak: number; empty: boolean } {
  const items: ColumnValues[] = [];
  let peak = 0;
  let empty = true;
  const last = input.times.length - 1;
  for (const column of layout.columns) {
    const raw = new Array<number>(seriesCount).fill(0);
    for (let k = column.first; k <= column.last; k++) {
      const row = input.rows[k];
      for (let s = 0; s < seriesCount; s++) {
        const v = row?.[s] ?? 0;
        if (v > 0) raw[s] += v;
      }
    }
    const count = column.last - column.first + 1;
    const scaleBy = count * input.divisor;
    const values = raw.map((v) => v / scaleBy);
    let rawTotal = 0;
    let barTotal = 0;
    for (let s = 0; s < seriesCount; s++) {
      rawTotal += raw[s];
      barTotal += values[s];
    }
    if (rawTotal > 0) empty = false;
    if (barTotal > peak) peak = barTotal;
    const t1 = column.last < last ? input.times[column.last + 1] : input.now;
    items.push({
      column,
      raw,
      rawTotal,
      count,
      values,
      t0: input.times[column.first],
      t1: Math.max(t1, input.times[column.last]),
    });
  }
  return { items, peak, empty };
}

export function buildTimeBars(input: TimeBarsModelInput): TimeBarsModel {
  const seriesCount = input.rows.reduce((n, row) => Math.max(n, row.length), 0);
  let gutter = gutterWidth([input.measure("0"), input.measure(formatCount(5, input.locale))]);
  let box = plotBox(input.width, input.height, gutter);
  let layout = layoutColumns(input.times, input.slots, input.bucketMs, box.width, input.now);
  let computed = columnValues(layout, input, seriesCount);
  const newestKey = input.times.length > 0 ? input.times[input.times.length - 1] : 0;
  let scale = nextScale(input.prevScale, computed.peak, newestKey);
  for (let pass = 0; pass < 3; pass++) {
    const labels = yTicks(scale.max).map((v) => formatCount(v, input.locale));
    const next = gutterWidth(labels.map((label) => input.measure(label)));
    if (next === gutter) break;
    gutter = next;
    box = plotBox(input.width, input.height, gutter);
    const relaid = layoutColumns(input.times, input.slots, input.bucketMs, box.width, input.now);
    if (relaid.factor !== layout.factor) {
      computed = columnValues(relaid, input, seriesCount);
      scale = nextScale(input.prevScale, computed.peak, newestKey);
    } else {
      computed = columnValues(relaid, input, seriesCount);
    }
    layout = relaid;
  }
  const columns: ColumnModel[] = computed.items.map((item) => ({
    ...item,
    stack: stackSegments(item.values, scale.max, box.height),
    open: item.column.open,
  }));
  const ticks: YTick[] = yTicks(scale.max).map((value) => ({
    value,
    label: formatCount(value, input.locale),
    row: valueRow(value, scale.max, box.height),
  }));
  const nowWidth = input.measure(input.nowLabel);
  const step = tickStepMinutes(layout.to - layout.from, box.width);
  const anchored = input.times.length > 0 || input.now > 0;
  const candidates = anchored ? localTicks(layout.from, layout.to, step) : [];
  const placed = placeXLabels(
    candidates,
    layout,
    (t) => input.measure(formatClock(t, input.locale)),
    -(box.left - CHART_PADDING / 2),
    box.width - nowWidth - 8,
  );
  return {
    box,
    layout,
    columns,
    scale,
    yTicks: ticks,
    gutter,
    empty: computed.empty,
    xLabels: placed.map((p) => ({ ...p, label: formatClock(p.t, input.locale) })),
    startX:
      input.startedAt !== undefined
        ? startMarkerX(layout, input.times, input.startedAt)
        : null,
  };
}

export function columnContaining(
  columns: readonly ColumnModel[],
  time: number,
): ColumnModel | undefined {
  for (const column of columns) {
    if (time >= column.t0 && (time < column.t1 || column.open)) return column;
  }
  return undefined;
}
