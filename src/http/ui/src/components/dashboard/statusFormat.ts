import type { TFunction } from "i18next";
import type { EngineState } from "../../models/metrics";

export const JUST_NOW_SECONDS = 1;
export const ONE_DECIMAL_BELOW = 10;

const FALLBACK_LOCALE = "en";
const percentFormats = new Map<string, Intl.NumberFormat>();

function percentFormat(locale: string, digits: number): Intl.NumberFormat {
  const key = `${locale}|${digits}`;
  let format = percentFormats.get(key);
  if (!format) {
    const options: Intl.NumberFormatOptions = {
      style: "percent",
      maximumFractionDigits: digits,
    };
    try {
      format = new Intl.NumberFormat(locale, options);
    } catch {
      format = new Intl.NumberFormat(FALLBACK_LOCALE, options);
    }
    percentFormats.set(key, format);
  }
  return format;
}

export function formatPercent(value: number, locale: string): string {
  if (!Number.isFinite(value) || value < 0) return "-";
  const digits = value > 0 && value < ONE_DECIMAL_BELOW ? 1 : 0;
  return percentFormat(locale, digits).format(value / 100);
}

export function sharePercent(part: number, total: number): number | null {
  if (!Number.isFinite(part) || !Number.isFinite(total)) return null;
  if (part <= 0 || total <= 0) return null;
  return (part / total) * 100;
}

function pair(t: TFunction, first: string, second: string | null): string {
  if (!second) return first;
  return t("dashboard.status.durationPair", { first, second });
}

export function formatUptime(totalSeconds: number, t: TFunction): string {
  const seconds = Number.isFinite(totalSeconds)
    ? Math.max(0, Math.floor(totalSeconds))
    : 0;
  if (seconds < 60) return t("charts.seconds", { n: seconds });
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return t("charts.minutes", { n: minutes });
  const hours = Math.floor(minutes / 60);
  if (hours < 24) {
    const rest = minutes % 60;
    return pair(
      t,
      t("charts.hours", { n: hours }),
      rest > 0 ? t("charts.minutes", { n: rest }) : null,
    );
  }
  const days = Math.floor(hours / 24);
  const rest = hours % 24;
  return pair(
    t,
    t("charts.days", { n: days }),
    rest > 0 ? t("charts.hours", { n: rest }) : null,
  );
}

export function formatAge(ageMs: number, t: TFunction): string {
  const seconds = Number.isFinite(ageMs) ? Math.floor(Math.max(0, ageMs) / 1000) : 0;
  if (seconds < JUST_NOW_SECONDS) return t("dashboard.common.justNow");
  let duration: string;
  if (seconds < 60) {
    duration = t("charts.seconds", { n: seconds });
  } else if (seconds < 3600) {
    duration = t("charts.minutes", { n: Math.floor(seconds / 60) });
  } else if (seconds < 48 * 3600) {
    duration = t("charts.hours", { n: Math.floor(seconds / 3600) });
  } else {
    duration = t("charts.days", { n: Math.floor(seconds / 86400) });
  }
  return t("dashboard.common.ago", { duration });
}

export type StateTone = "success" | "info" | "warning" | "error";

export const STATE_TONE: Record<EngineState, StateTone> = {
  running: "success",
  starting: "info",
  stopping: "warning",
  failed: "error",
};

export function effectiveState(
  state: EngineState | undefined,
  failed: boolean,
): EngineState {
  if (failed) return "failed";
  if (state === "starting" || state === "stopping" || state === "failed") {
    return state;
  }
  return "running";
}

export function engineName(mode: string | undefined): string {
  if (mode === "tun") return "TUN";
  if (mode === "nfqueue") return "NFQUEUE";
  return mode ? mode.toUpperCase() : "-";
}
