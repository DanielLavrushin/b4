const FALLBACK_LOCALE = "en";

const numberFormats = new Map<string, Intl.NumberFormat>();
const dateFormats = new Map<string, Intl.DateTimeFormat>();

function numberFormat(
  locale: string,
  key: string,
  options: Intl.NumberFormatOptions,
): Intl.NumberFormat {
  const cacheKey = `${locale}|${key}`;
  let format = numberFormats.get(cacheKey);
  if (!format) {
    try {
      format = new Intl.NumberFormat(locale, options);
    } catch {
      format = new Intl.NumberFormat(FALLBACK_LOCALE, options);
    }
    numberFormats.set(cacheKey, format);
  }
  return format;
}

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

function roundTo(value: number, digits: number): number {
  const scale = 10 ** digits;
  return Math.round(value * scale) / scale;
}

export function formatCount(
  value: number,
  locale: string,
  maxFraction = 0,
): string {
  if (!Number.isFinite(value)) return "-";
  const rounded = roundTo(Math.abs(value), maxFraction);
  const signed = rounded === 0 ? 0 : Math.sign(value) * rounded;
  if (rounded < 1000) {
    return numberFormat(locale, `plain${maxFraction}`, {
      maximumFractionDigits: maxFraction,
    }).format(signed);
  }
  return numberFormat(locale, "compact", {
    notation: "compact",
    maximumFractionDigits: 1,
  }).format(signed);
}

export function formatInteger(value: number, locale: string): string {
  if (!Number.isFinite(value)) return "-";
  const rounded = Math.round(value);
  return numberFormat(locale, "integer", { maximumFractionDigits: 0 }).format(
    rounded === 0 ? 0 : rounded,
  );
}

const BYTE_UNITS = [
  "byte",
  "kilobyte",
  "megabyte",
  "gigabyte",
  "terabyte",
] as const;

export function formatByteSize(bytes: number, locale: string): string {
  if (!Number.isFinite(bytes) || bytes < 0) return "-";
  let unit = 0;
  let value = bytes;
  while (unit < BYTE_UNITS.length - 1 && value >= 1024) {
    value /= 1024;
    unit++;
  }
  let digits = unit === 0 || value >= 100 ? 0 : 1;
  let rounded = roundTo(value, digits);
  if (rounded >= 1024 && unit < BYTE_UNITS.length - 1) {
    unit++;
    value = rounded / 1024;
    digits = value >= 100 ? 0 : 1;
    rounded = roundTo(value, digits);
  } else if (digits === 1 && rounded >= 100) {
    digits = 0;
    rounded = roundTo(value, 0);
  }
  const format = numberFormat(locale, `unit-${BYTE_UNITS[unit]}-${digits}`, {
    style: "unit",
    unit: BYTE_UNITS[unit],
    unitDisplay: "short",
    maximumFractionDigits: digits,
  });
  if (unit > 0) return format.format(rounded);
  return format
    .formatToParts(rounded)
    .map((part) =>
      part.type === "unit" && /^bytes?$/.test(part.value) ? "B" : part.value,
    )
    .join("");
}

export function formatClock(
  ms: number,
  locale: string,
  withSeconds = false,
): string {
  if (!Number.isFinite(ms) || ms <= 0) return "-";
  const format = withSeconds
    ? dateFormat(locale, "clock-s", {
        hour: "2-digit",
        minute: "2-digit",
        second: "2-digit",
        hourCycle: "h23",
      })
    : dateFormat(locale, "clock", {
        hour: "2-digit",
        minute: "2-digit",
        hourCycle: "h23",
      });
  return format.format(new Date(ms));
}

export function formatClockRange(
  from: number,
  to: number,
  locale: string,
): string {
  return `${formatClock(from, locale)}-${formatClock(to, locale)}`;
}

export type DurationUnit = "seconds" | "minutes" | "hours" | "days";

export interface DurationPart {
  unit: DurationUnit;
  count: number;
}

export function splitDuration(ms: number): DurationPart {
  const seconds = Math.max(0, Math.floor(ms / 1000));
  if (seconds < 60) return { unit: "seconds", count: seconds };
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return { unit: "minutes", count: minutes };
  const hours = Math.floor(minutes / 60);
  if (hours < 48) return { unit: "hours", count: hours };
  return { unit: "days", count: Math.floor(hours / 24) };
}
