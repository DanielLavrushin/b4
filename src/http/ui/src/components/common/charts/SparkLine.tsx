import { memo, useMemo, type ReactNode } from "react";
import { colors } from "@design";
import { niceCeilReal } from "./scale";
import { sparkPoints } from "./geometry";

export const SPARK_WINDOW_MS = 24 * 3_600_000;
export const SPARK_BUCKET_MS = 600_000;

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
  width?: number;
  height?: number;
  unit?: number;
  color?: string;
  fallback?: ReactNode;
  stale?: boolean;
}

function SparkLineChart<K extends string>({
  buckets,
  valueKey,
  now,
  ariaLabel,
  windowMs = SPARK_WINDOW_MS,
  bucketMs = SPARK_BUCKET_MS,
  width = 72,
  height = 20,
  unit = 1,
  color = colors.text.secondary,
  fallback = null,
  stale = false,
}: SparkLineProps<K>) {
  const points = useMemo(() => {
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
    const ceiling = niceCeilReal(max / safeUnit) * safeUnit;
    return sparkPoints({ times, values, now, windowMs, bucketMs, width, height, ceiling });
  }, [buckets, valueKey, now, windowMs, bucketMs, width, height, unit]);

  if (!points) return <>{fallback}</>;

  return (
    <svg
      width={width}
      height={height}
      role="img"
      aria-label={ariaLabel}
      style={{ display: "block", flexShrink: 0, opacity: stale ? 0.5 : 1 }}
    >
      <polyline
        points={points}
        fill="none"
        stroke={color}
        strokeWidth={1.5}
        vectorEffect="non-scaling-stroke"
        strokeLinejoin="round"
        strokeLinecap="round"
      />
    </svg>
  );
}

export const SparkLine = memo(SparkLineChart) as typeof SparkLineChart;
