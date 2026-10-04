import {
  memo,
  useEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type KeyboardEvent,
  type PointerEvent,
} from "react";
import {
  Box,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
} from "@mui/material";
import { useTranslation } from "react-i18next";
import { colors, fonts, radiusPx } from "@design";
import {
  formatClock,
  formatClockRange,
  formatCount,
  formatInteger,
  splitDuration,
} from "./format";
import type { ScaleState } from "./scale";
import {
  AXIS_FONT_PX,
  LABEL_GAP,
  X_AXIS_BAND,
  barRadius,
  columnAt,
  placeStartLabel,
  summarizeRows,
  topRoundedPath,
} from "./geometry";
import {
  buildTimeBars,
  columnContaining,
  type ColumnModel,
  type TimeBarsModel,
} from "./timeBarsModel";
import { measureText, useElementWidth } from "./measure";

export interface TimeBarsSeries<K extends string = string> {
  key: K;
  label: string;
  color: string;
}

export type TimeBarsBucket<K extends string = string> = {
  readonly t: number;
} & { readonly [P in K]: number };

export interface TimeBarsSummary {
  label: string;
  empty: boolean;
  totals: { label: string; value: number }[];
  busiest: { from: number; to: number; value: number } | null;
}

export interface TimeBarsProps<K extends string> {
  buckets: readonly TimeBarsBucket<K>[];
  series: readonly TimeBarsSeries<K>[];
  slots: number;
  bucketMs: number;
  now: number;
  summaryLabel: string;
  emptyText: string;
  height?: number;
  divisor?: number;
  formatValue?: (raw: number, bucketsInBar: number, spanMs: number) => string;
  startedAt?: number;
  startLabel?: string;
  stale?: boolean;
  table?: boolean;
  describe?: (summary: TimeBarsSummary) => string;
}

const DEFAULT_HEIGHT = 180;
const READOUT_OFFSET = 6;

const axisText: CSSProperties = { fontVariantNumeric: "tabular-nums" };

const visuallyHidden: CSSProperties = {
  position: "absolute",
  width: 1,
  height: 1,
  margin: -1,
  padding: 0,
  overflow: "hidden",
  clip: "rect(0 0 0 0)",
  whiteSpace: "nowrap",
  border: 0,
};

const measureAxis = (text: string) => measureText(text, AXIS_FONT_PX, fonts.mono);

const numericCellSx = { fontVariantNumeric: "tabular-nums" } as const;
const labelCellSx = { fontVariantNumeric: "tabular-nums", whiteSpace: "nowrap" } as const;

interface BucketRowProps {
  label: string;
  keys: readonly string[];
  values: readonly number[];
  locale: string;
}

function sameBucketRow(a: BucketRowProps, b: BucketRowProps): boolean {
  if (a.label !== b.label || a.locale !== b.locale) return false;
  if (a.keys.length !== b.keys.length || a.values.length !== b.values.length) return false;
  for (let i = 0; i < a.keys.length; i++) {
    if (a.keys[i] !== b.keys[i]) return false;
  }
  for (let i = 0; i < a.values.length; i++) {
    if (!Object.is(a.values[i], b.values[i])) return false;
  }
  return true;
}

const BucketRow = memo(function BucketRow({ label, keys, values, locale }: BucketRowProps) {
  const total = values.reduce((sum, v) => sum + (v > 0 ? v : 0), 0);
  return (
    <TableRow>
      <TableCell sx={labelCellSx}>{label}</TableCell>
      {keys.map((key, i) => (
        <TableCell key={key} align="right" sx={numericCellSx}>
          {formatInteger(values[i] ?? 0, locale)}
        </TableCell>
      ))}
      <TableCell align="right" sx={numericCellSx}>
        {formatInteger(total, locale)}
      </TableCell>
    </TableRow>
  );
}, sameBucketRow);

function TimeBarsChart<K extends string>({
  buckets,
  series,
  slots,
  bucketMs,
  now,
  summaryLabel,
  emptyText,
  height = DEFAULT_HEIGHT,
  divisor = 1,
  formatValue,
  startedAt,
  startLabel,
  stale = false,
  table = false,
  describe,
}: TimeBarsProps<K>) {
  const { t, i18n } = useTranslation();
  const locale = i18n.language;
  const [containerRef, width] = useElementWidth<HTMLDivElement>();
  const scaleRef = useRef<{ domain: string; state: ScaleState } | null>(null);
  const summaryRef = useRef<{ key: string; text: string } | null>(null);
  const [active, setActive] = useState<number | null>(null);
  const [pinned, setPinned] = useState(false);
  const [liveText, setLiveText] = useState("");
  const safeDivisor = divisor > 0 ? divisor : 1;
  const nowLabel = t("charts.now");

  const times = useMemo(() => buckets.map((b) => b.t), [buckets]);
  const rows = useMemo(
    (): number[][] =>
      buckets.map((b) =>
        series.map((s): number => {
          const value: number = b[s.key];
          return Number.isFinite(value) ? value : 0;
        }),
      ),
    [buckets, series],
  );

  const domain = `${slots}|${bucketMs}|${safeDivisor}|${width}`;
  const previousScale = scaleRef.current?.domain === domain ? scaleRef.current.state : null;
  const model: TimeBarsModel | null = useMemo(
    () =>
      width > 0
        ? buildTimeBars({
            width,
            height,
            times,
            rows,
            slots,
            bucketMs,
            now,
            divisor: safeDivisor,
            locale,
            startedAt,
            prevScale: previousScale,
            measure: measureAxis,
            nowLabel,
          })
        : null,
    [width, height, times, rows, slots, bucketMs, now, safeDivisor, locale, startedAt, previousScale, nowLabel],
  );
  if (model) scaleRef.current = { domain, state: model.scale };

  const duration = (ms: number) => {
    const part = splitDuration(ms);
    return t(`charts.${part.unit}`, { n: part.count });
  };

  const elapsed = (ms: number) => {
    const seconds = Math.max(0, Math.floor(ms / 1000));
    if (seconds < 60 || seconds >= 3600) return duration(ms);
    const minutes = t("charts.minutes", { n: Math.floor(seconds / 60) });
    const rest = seconds % 60;
    if (rest === 0) return minutes;
    return t("charts.durationPair", {
      first: minutes,
      second: t("charts.seconds", { n: rest }),
    });
  };

  const summaryKey = `${times[times.length - 1] ?? 0}|${times.length}|${slots}|${bucketMs}|${locale}|${summaryLabel}|${emptyText}|${series
    .map((s) => s.label)
    .join("|")}`;
  if (!summaryRef.current || summaryRef.current.key !== summaryKey) {
    const stats = summarizeRows(rows.length > 1 ? rows.slice(0, -1) : rows);
    const busiest =
      stats.busiest >= 0
        ? {
            from: times[stats.busiest],
            to: times[stats.busiest + 1] ?? now,
            value: stats.busiestTotal,
          }
        : null;
    const summary: TimeBarsSummary = {
      label: summaryLabel,
      empty: stats.allZero,
      totals: series.map((s, i) => ({ label: s.label, value: stats.totals[i] ?? 0 })),
      busiest,
    };
    let text: string;
    if (describe) text = describe(summary);
    else if (summary.empty) text = `${summaryLabel}: ${emptyText}`;
    else {
      const parts = summary.totals.map((item) => `${formatInteger(item.value, locale)} ${item.label}`);
      if (busiest) {
        parts.push(
          t("charts.busiest", {
            range: formatClockRange(busiest.from, busiest.to, locale),
            value: formatInteger(busiest.value, locale),
          }),
        );
      }
      text = `${summaryLabel}: ${parts.join(", ")}`;
    }
    summaryRef.current = { key: summaryKey, text };
  }
  const ariaSummary = summaryRef.current.text;

  const activeColumn: ColumnModel | undefined =
    model && active !== null ? columnContaining(model.columns, active) : undefined;

  useEffect(() => {
    if (!pinned) return;
    const onDown = (event: globalThis.PointerEvent) => {
      const container = containerRef.current;
      if (container && event.target instanceof Node && container.contains(event.target)) return;
      setPinned(false);
      setActive(null);
    };
    document.addEventListener("pointerdown", onDown, true);
    return () => document.removeEventListener("pointerdown", onDown, true);
  }, [pinned, containerRef]);

  const valueText = (raw: number, column: ColumnModel) =>
    formatValue
      ? formatValue(
          raw,
          column.count,
          Math.max(0, Math.min(column.t1 - column.t0, column.count * bucketMs)),
        )
      : formatCount(raw, locale);

  const rangeText = (column: ColumnModel) =>
    column.open
      ? t("charts.openRange", {
          start: formatClock(column.t0, locale),
          elapsed: elapsed(now - column.t0),
        })
      : formatClockRange(column.t0, column.t1, locale);

  const readoutLines = (column: ColumnModel): string[] => {
    const lines = [rangeText(column)];
    series.forEach((s, i) => lines.push(`${valueText(column.raw[i] ?? 0, column)} ${s.label}`));
    lines.push(t("charts.total", { value: valueText(column.rawTotal, column) }));
    if (column.count > 1) {
      lines.push(t("charts.mergedNote", { duration: duration(column.count * bucketMs) }));
    }
    return lines;
  };

  const pick = (clientX: number, clientY: number, element: HTMLElement): number | null => {
    if (!model) return null;
    const rect = element.getBoundingClientRect();
    const y = clientY - rect.top;
    if (y < 0 || y > model.box.baseline + 1) return null;
    const column = columnAt(model.layout, clientX - rect.left - model.box.left);
    if (!column) return null;
    return model.columns.find((c) => c.column === column)?.t0 ?? null;
  };

  const onPointerMove = (event: PointerEvent<HTMLDivElement>) => {
    if (pinned && event.pointerType !== "mouse") return;
    setActive(pick(event.clientX, event.clientY, event.currentTarget));
  };

  const onPointerLeave = () => {
    if (!pinned) setActive(null);
  };

  const onPointerDown = (event: PointerEvent<HTMLDivElement>) => {
    if (event.pointerType === "mouse") return;
    const hit = pick(event.clientX, event.clientY, event.currentTarget);
    setActive(hit);
    setPinned(hit !== null);
  };

  const navigate = (next: ColumnModel | undefined) => {
    if (!next) return;
    setActive(next.t0);
    setLiveText(readoutLines(next).join(". "));
  };

  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (!model || model.columns.length === 0) return;
    const list = model.columns;
    const index = activeColumn ? list.indexOf(activeColumn) : list.length;
    switch (event.key) {
      case "ArrowLeft":
        navigate(list[Math.max(0, index - 1)]);
        break;
      case "ArrowRight":
        navigate(list[Math.min(list.length - 1, index + 1)]);
        break;
      case "Home":
        navigate(list[0]);
        break;
      case "End":
        navigate(list[list.length - 1]);
        break;
      case "Escape":
        setActive(null);
        setPinned(false);
        setLiveText("");
        break;
      default:
        return;
    }
    event.preventDefault();
  };

  const onFocus = () => {
    if (active === null && model && model.columns.length > 0) {
      setActive(model.columns[model.columns.length - 1].t0);
    }
  };

  const onBlur = () => {
    if (pinned) return;
    setActive(null);
    setLiveText("");
  };

  const textProps = {
    fontFamily: fonts.mono,
    fontSize: AXIS_FONT_PX,
    fill: colors.text.secondary,
    style: axisText,
  } as const;

  const renderBars = (m: TimeBarsModel) =>
    m.columns.map((column) => {
      const { stack } = column;
      if (stack.top === null) return null;
      let topIndex = -1;
      stack.segments.forEach((segment, i) => {
        if (segment) topIndex = i;
      });
      const x = m.box.left + column.column.x;
      const w = column.column.w;
      return (
        <g key={column.t0}>
          {stack.segments.map((segment, i) => {
            if (!segment) return null;
            const y = m.box.top + segment.y;
            const color = series[i]?.color ?? colors.text.secondary;
            if (i === topIndex) {
              return (
                <path
                  key={series[i]?.key ?? i}
                  d={topRoundedPath(x, y, w, segment.h, barRadius(w))}
                  fill={color}
                />
              );
            }
            return (
              <rect key={series[i]?.key ?? i} x={x} y={y} width={w} height={segment.h} fill={color} />
            );
          })}
          {column.open && (
            <rect
              x={x + 0.5}
              y={m.box.top + (stack.top ?? 0) + 0.5}
              width={Math.max(0, w - 1)}
              height={Math.max(0, m.box.height - (stack.top ?? 0) - 1)}
              fill="none"
              stroke={colors.text.secondary}
              strokeWidth={1}
              strokeDasharray="2 2"
            />
          )}
        </g>
      );
    });

  const renderReadout = (m: TimeBarsModel, column: ColumnModel) => {
    const slotLeft = m.box.left + column.column.slotX;
    const slotRight = slotLeft + column.column.slotW;
    const onLeftHalf = slotLeft + column.column.slotW / 2 < width / 2;
    const placement: CSSProperties = onLeftHalf
      ? { left: slotRight + READOUT_OFFSET, maxWidth: Math.max(96, width - slotRight - READOUT_OFFSET - 4) }
      : { right: width - slotLeft + READOUT_OFFSET, maxWidth: Math.max(96, slotLeft - READOUT_OFFSET - 4) };
    const lines = readoutLines(column);
    return (
      <Box
        aria-hidden="true"
        sx={{
          position: "absolute",
          top: m.box.top,
          zIndex: 1,
          pointerEvents: "none",
          bgcolor: colors.background.dark,
          border: `1px solid ${colors.border.default}`,
          borderRadius: `${radiusPx.sm}px`,
          px: 1,
          py: 0.75,
          fontFamily: fonts.sans,
          fontSize: 12,
          lineHeight: 1.45,
          color: colors.text.primary,
          fontVariantNumeric: "tabular-nums",
        }}
        style={placement}
      >
        <Box sx={{ color: colors.text.secondary }}>{lines[0]}</Box>
        {series.map((s, i) => (
          <Box key={s.key} sx={{ display: "flex", alignItems: "center", gap: 0.75 }}>
            <Box
              component="span"
              sx={{ width: 8, height: 8, borderRadius: "2px", bgcolor: s.color, flexShrink: 0 }}
            />
            <span>{lines[i + 1]}</span>
          </Box>
        ))}
        <Box sx={{ fontWeight: 600 }}>{lines[series.length + 1]}</Box>
        {column.count > 1 && (
          <Box sx={{ color: colors.text.secondary, fontSize: 11 }}>{lines[series.length + 2]}</Box>
        )}
      </Box>
    );
  };

  const renderChart = (m: TimeBarsModel) => {
    const startPlacement =
      m.startX !== null && startLabel
        ? placeStartLabel(m.startX, measureAxis(startLabel), m.box.width)
        : null;
    return (
      <>
        <svg
          width={width}
          height={height}
          shapeRendering="crispEdges"
          aria-hidden="true"
          focusable="false"
          style={{ display: "block", overflow: "visible" }}
        >
          {!m.empty &&
            m.yTicks
              .filter((tick) => tick.value > 0)
              .map((tick) => (
                <rect
                  key={`g${tick.value}`}
                  x={m.box.left}
                  y={m.box.top + tick.row}
                  width={m.box.width}
                  height={1}
                  fill={colors.border.light}
                />
              ))}
          {activeColumn && (
            <rect
              x={m.box.left + activeColumn.column.slotX}
              y={m.box.top}
              width={activeColumn.column.slotW}
              height={m.box.height}
              fill={colors.accent.secondaryHover}
            />
          )}
          {renderBars(m)}
          <rect
            x={m.box.left}
            y={m.box.baseline}
            width={m.box.width}
            height={1}
            fill={colors.border.default}
          />
          {m.startX !== null && (
            <rect
              x={m.box.left + m.startX}
              y={m.box.top}
              width={1}
              height={m.box.height}
              fill={colors.text.disabled}
            />
          )}
          {startPlacement && (
            <text
              {...textProps}
              x={m.box.left + startPlacement.x}
              y={m.box.top + AXIS_FONT_PX}
              textAnchor={startPlacement.anchor}
            >
              {startLabel}
            </text>
          )}
          {!m.empty &&
            m.yTicks.map((tick) => (
              <text
                key={`y${tick.value}`}
                {...textProps}
                x={m.box.left - LABEL_GAP}
                y={m.box.top + tick.row + 3.5}
                textAnchor="end"
              >
                {tick.label}
              </text>
            ))}
          {m.xLabels.map((label) => (
            <text
              key={`x${label.t}`}
              {...textProps}
              x={m.box.left + label.x}
              y={m.box.baseline + X_AXIS_BAND - 3}
              textAnchor="middle"
            >
              {label.label}
            </text>
          ))}
          <text
            {...textProps}
            x={m.box.right}
            y={m.box.baseline + X_AXIS_BAND - 3}
            textAnchor="end"
          >
            {nowLabel}
          </text>
        </svg>
        {m.empty && (
          <Box
            sx={{
              position: "absolute",
              left: m.box.left,
              top: m.box.top,
              width: m.box.width,
              height: m.box.height,
              display: "flex",
              alignItems: "center",
              justifyContent: "center",
              textAlign: "center",
              px: 1,
              color: colors.text.secondary,
              fontFamily: fonts.sans,
              fontSize: 13,
              pointerEvents: "none",
            }}
          >
            {emptyText}
          </Box>
        )}
        {activeColumn && renderReadout(m, activeColumn)}
      </>
    );
  };

  const renderTable = () => {
    const order = buckets.map((_, i) => buckets.length - 1 - i);
    const keys = series.map((s) => s.key);
    return (
      <Box sx={{ height, overflowY: "auto" }}>
        <Table size="small" stickyHeader aria-label={ariaSummary}>
          <TableHead>
            <TableRow>
              <TableCell sx={{ bgcolor: colors.background.paper }}>{t("charts.time")}</TableCell>
              {series.map((s) => (
                <TableCell key={s.key} align="right" sx={{ bgcolor: colors.background.paper }}>
                  {s.label}
                </TableCell>
              ))}
              <TableCell align="right" sx={{ bgcolor: colors.background.paper }}>
                {t("charts.totalColumn")}
              </TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {order.map((i) => {
              const isOpen = i === buckets.length - 1;
              const from = times[i];
              const label = isOpen
                ? t("charts.openRange", {
                    start: formatClock(from, locale),
                    elapsed: elapsed(now - from),
                  })
                : formatClockRange(from, times[i + 1] ?? from + bucketMs, locale);
              return (
                <BucketRow key={from} label={label} keys={keys} values={rows[i]} locale={locale} />
              );
            })}
          </TableBody>
        </Table>
      </Box>
    );
  };

  return (
    <Box
      ref={containerRef}
      sx={{
        position: "relative",
        width: "100%",
        minWidth: 0,
        height: table ? "auto" : height,
        opacity: stale ? 0.5 : 1,
      }}
    >
      {table ? (
        renderTable()
      ) : (
        <>
          <Box
            role="group"
            tabIndex={0}
            aria-label={ariaSummary}
            onPointerMove={onPointerMove}
            onPointerLeave={onPointerLeave}
            onPointerDown={onPointerDown}
            onKeyDown={onKeyDown}
            onFocus={onFocus}
            onBlur={onBlur}
            sx={{
              position: "absolute",
              inset: 0,
              outline: "none",
              userSelect: "none",
              touchAction: "pan-y",
              borderRadius: `${radiusPx.sm}px`,
              "&:focus-visible": {
                boxShadow: `inset 0 0 0 2px ${colors.border.strong}`,
              },
            }}
          >
            {model && renderChart(model)}
          </Box>
          <span aria-live="polite" aria-atomic="true" style={visuallyHidden}>
            {liveText}
          </span>
        </>
      )}
    </Box>
  );
}

export const TimeBars = memo(TimeBarsChart) as typeof TimeBarsChart;
