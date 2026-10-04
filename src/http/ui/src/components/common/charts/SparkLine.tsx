import { memo, useMemo, useState, type PointerEvent, type ReactNode } from "react";
import { Box, Tooltip } from "@mui/material";
import { colors } from "@design";
import { niceCeilReal } from "./scale";
import { nearestCoord, sparkCoords } from "./geometry";
import { useElementWidth } from "./measure";

export const SPARK_WINDOW_MS = 24 * 3_600_000;
export const SPARK_BUCKET_MS = 600_000;

const AREA_OPACITY = 0.08;
const HEADROOM = 1.25;

export type SparkLineBucket<K extends string = string> = {
  readonly t: number;
} & { readonly [P in K]: number };

export interface SparkLineProps<K extends string> {
  buckets: readonly SparkLineBucket<K>[];
  valueKey: K;
  now: number;
  ariaLabel: string;
  windowMs?: number;
  bucketMs?: number;
  width?: number | "fill";
  height?: number;
  unit?: number;
  color?: string;
  area?: boolean;
  readout?: (bucket: SparkLineBucket<K>) => string;
  fallback?: ReactNode;
  stale?: boolean;
}

type SparkSvgProps<K extends string> = Omit<SparkLineProps<K>, "width"> & {
  width: number;
};

function SparkSvg<K extends string>({
  buckets,
  valueKey,
  now,
  ariaLabel,
  windowMs = SPARK_WINDOW_MS,
  bucketMs = SPARK_BUCKET_MS,
  width,
  height = 20,
  unit = 1,
  color = colors.text.secondary,
  area = false,
  readout,
  fallback = null,
  stale = false,
}: SparkSvgProps<K>) {
  const [hover, setHover] = useState<number | null>(null);
  const coords = useMemo(() => {
    const from = now - windowMs;
    const times: number[] = [];
    const values: number[] = [];
    let max = 0;
    for (const bucket of buckets) {
      const value = Number.isFinite(bucket[valueKey]) ? bucket[valueKey] : 0;
      times.push(bucket.t);
      values.push(value);
      if (bucket.t + bucketMs / 2 >= from && value > max) max = value;
    }
    const safeUnit = unit > 0 ? unit : 1;
    const ceiling = niceCeilReal((max * HEADROOM) / safeUnit) * safeUnit;
    return sparkCoords({ times, values, now, windowMs, bucketMs, width, height, ceiling });
  }, [buckets, valueKey, now, windowMs, bucketMs, width, height, unit]);

  if (!coords) return <>{fallback}</>;

  const points = coords.map((c) => `${c.x},${c.y}`).join(" ");
  const baseline = height - 1;
  const hovered = hover !== null && hover < coords.length ? coords[hover] : null;

  const onPointerMove = (event: PointerEvent<SVGSVGElement>) => {
    const rect = event.currentTarget.getBoundingClientRect();
    setHover(nearestCoord(coords, event.clientX - rect.left));
  };

  const svg = (
    <svg
      width={width}
      height={height}
      role="img"
      aria-label={ariaLabel}
      onPointerMove={readout ? onPointerMove : undefined}
      onPointerLeave={readout ? () => setHover(null) : undefined}
      style={{ display: "block", flexShrink: 0, opacity: stale ? 0.5 : 1 }}
    >
      {area && (
        <polygon
          points={`${coords[0].x},${baseline} ${points} ${coords[coords.length - 1].x},${baseline}`}
          fill={color}
          fillOpacity={AREA_OPACITY}
        />
      )}
      <polyline
        points={points}
        fill="none"
        stroke={color}
        strokeWidth={1.5}
        vectorEffect="non-scaling-stroke"
        strokeLinejoin="round"
        strokeLinecap="round"
      />
      {hovered && (
        <>
          <line
            x1={hovered.x}
            x2={hovered.x}
            y1={0}
            y2={height}
            stroke={colors.border.strong}
            strokeWidth={1}
            vectorEffect="non-scaling-stroke"
          />
          <circle cx={hovered.x} cy={hovered.y} r={2.5} fill={color} />
        </>
      )}
    </svg>
  );

  if (!readout) return svg;
  return (
    <Tooltip
      followCursor
      placement="top"
      title={hovered ? readout(buckets[hovered.index]) : ""}
    >
      {svg}
    </Tooltip>
  );
}

function SparkFill<K extends string>(props: SparkSvgProps<K>) {
  const [ref, measured] = useElementWidth<HTMLDivElement>();
  return (
    <Box ref={ref} sx={{ width: "100%", minWidth: 0, height: props.height ?? 20 }}>
      {measured > 0 && <SparkSvg {...props} width={measured} />}
    </Box>
  );
}

function SparkLineChart<K extends string>({ width = 72, ...rest }: SparkLineProps<K>) {
  if (width === "fill") return <SparkFill {...rest} width={0} />;
  return <SparkSvg {...rest} width={width} />;
}

export const SparkLine = memo(SparkLineChart) as typeof SparkLineChart;
