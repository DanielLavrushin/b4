import { formatClock, splitDuration } from "../../common/charts/format";
import type { DurationPart } from "../../common/charts/format";

export const JUST_NOW_MS = 10_000;

const FALLBACK_LOCALE = "en";
const dateFormats = new Map<string, Intl.DateTimeFormat>();

function dateFormat(
  locale: string,
  key: string,
  options: Intl.DateTimeFormatOptions,
): Intl.DateTimeFormat {
  const cacheKey = `${locale}|${key}`;
  let format = dateFormats.get(cacheKey);
  if (!format) {
    try {
      format = new Intl.DateTimeFormat(locale, options);
    } catch {
      format = new Intl.DateTimeFormat(FALLBACK_LOCALE, options);
    }
    dateFormats.set(cacheKey, format);
  }
  return format;
}

export function validTime(ms: number | undefined | null): ms is number {
  return typeof ms === "number" && Number.isFinite(ms) && ms > 0;
}

const DATE_TIME: Intl.DateTimeFormatOptions = {
  year: "numeric",
  month: "short",
  day: "numeric",
  hour: "2-digit",
  minute: "2-digit",
  hourCycle: "h23",
};

export function formatDateTime(
  ms: number,
  locale: string,
  withSeconds = true,
): string {
  if (!validTime(ms)) return "-";
  const format = withSeconds
    ? dateFormat(locale, "datetime-s", { ...DATE_TIME, second: "2-digit" })
    : dateFormat(locale, "datetime", DATE_TIME);
  return format.format(new Date(ms));
}

export function sameLocalDay(a: number, b: number): boolean {
  const x = new Date(a);
  const y = new Date(b);
  return (
    x.getFullYear() === y.getFullYear() &&
    x.getMonth() === y.getMonth() &&
    x.getDate() === y.getDate()
  );
}

export function formatSince(ms: number, now: number, locale: string): string {
  if (!validTime(ms)) return "-";
  if (!validTime(now) || sameLocalDay(ms, now)) return formatClock(ms, locale);
  return dateFormat(locale, "day-clock", {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    hourCycle: "h23",
  }).format(new Date(ms));
}

export function parseTimestamp(value: string | undefined | null): number {
  if (!value) return 0;
  const ms = Date.parse(value);
  return validTime(ms) ? ms : 0;
}

export function agePart(diffMs: number): DurationPart | null {
  if (!(diffMs >= JUST_NOW_MS)) return null;
  return splitDuration(diffMs);
}

export interface RemainingParts {
  hours: number;
  minutes: number;
  seconds: number;
}

export function splitRemaining(ms: number): RemainingParts | null {
  if (!(ms > 0)) return null;
  const totalSeconds = Math.ceil(ms / 1000);
  if (totalSeconds < 60) return { hours: 0, minutes: 0, seconds: totalSeconds };
  const totalMinutes = Math.ceil(totalSeconds / 60);
  return {
    hours: Math.floor(totalMinutes / 60),
    minutes: totalMinutes % 60,
    seconds: 0,
  };
}

export function parseCount(value: string | undefined): number | null {
  if (value === undefined || value === "") return null;
  const n = Number(value);
  return Number.isFinite(n) ? n : null;
}

export interface BucketTotals {
  in_sets: number;
  not_in_set: number;
}

export function sumBuckets(
  buckets: readonly { in_sets: number; not_in_set: number }[],
): BucketTotals {
  let inSets = 0;
  let notInSet = 0;
  for (const bucket of buckets) {
    if (Number.isFinite(bucket.in_sets) && bucket.in_sets > 0) inSets += bucket.in_sets;
    if (Number.isFinite(bucket.not_in_set) && bucket.not_in_set > 0) {
      notInSet += bucket.not_in_set;
    }
  }
  return { in_sets: inSets, not_in_set: notInSet };
}
