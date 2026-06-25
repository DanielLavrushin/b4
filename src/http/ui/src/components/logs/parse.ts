export type LogLevel = "error" | "warn" | "info" | "trace" | "debug";

export const LOG_LEVELS: LogLevel[] = [
  "error",
  "warn",
  "info",
  "trace",
  "debug",
];

export interface ParsedLogLine {
  raw: string;
  level: LogLevel | null;
  date: string | null;
  time: string | null;
  message: string;
}

const LINE_RE =
  /^(\d{4}\/\d{2}\/\d{2}) (\d{2}:\d{2}:\d{2}(?:\.\d+)?) \[(ERROR|WARN|INFO|TRACE|DEBUG)\] ([\s\S]*)$/;

const TAG_TO_LEVEL: Record<string, LogLevel> = {
  ERROR: "error",
  WARN: "warn",
  INFO: "info",
  TRACE: "trace",
  DEBUG: "debug",
};

export function parseLogLine(raw: string): ParsedLogLine {
  const m = LINE_RE.exec(raw);
  if (!m) {
    return { raw, level: null, date: null, time: null, message: raw };
  }
  return {
    raw,
    level: TAG_TO_LEVEL[m[3]],
    date: m[1],
    time: m[2],
    message: m[4],
  };
}

export const LEVELS_STORAGE_KEY = "b4_logs_levels";

export function loadEnabledLevels(): Set<LogLevel> {
  try {
    const stored = localStorage.getItem(LEVELS_STORAGE_KEY);
    if (stored) {
      const parsed = JSON.parse(stored) as unknown;
      if (Array.isArray(parsed)) {
        const valid = parsed.filter(
          (v): v is LogLevel => LOG_LEVELS.includes(v as LogLevel),
        );
        return new Set(valid);
      }
    }
  } catch (e) {
    console.error("Failed to load log level filter:", e);
  }
  return new Set(LOG_LEVELS);
}

export function saveEnabledLevels(levels: Set<LogLevel>): void {
  try {
    localStorage.setItem(LEVELS_STORAGE_KEY, JSON.stringify([...levels]));
  } catch (e) {
    console.error("Failed to save log level filter:", e);
  }
}
