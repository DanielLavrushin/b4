import { memo } from "react";
import { colors } from "@design";
import { LogLevel, ParsedLogLine } from "./parse";

interface RowTheme {
  border: string;
  tint: string;
  text: string;
}

const rowTheme: Record<LogLevel, RowTheme> = {
  error: {
    border: colors.state.error,
    tint: "rgba(244, 67, 54, 0.10)",
    text: colors.text.primary,
  },
  warn: {
    border: colors.state.warning,
    tint: "rgba(255, 167, 38, 0.08)",
    text: colors.text.primary,
  },
  info: {
    border: "rgba(245, 173, 24, 0.20)",
    tint: "transparent",
    text: colors.text.primary,
  },
  trace: {
    border: "rgba(255, 255, 255, 0.08)",
    tint: "transparent",
    text: colors.text.disabled,
  },
  debug: {
    border: "rgba(255, 255, 255, 0.08)",
    tint: "transparent",
    text: colors.text.disabled,
  },
};

const unparsedTheme: RowTheme = {
  border: "rgba(245, 173, 24, 0.20)",
  tint: "transparent",
  text: colors.text.primary,
};

const themeSx = (theme: RowTheme) => ({
  borderLeftColor: theme.border,
  bgcolor: theme.tint,
  color: theme.text,
});

export const logRowSx = {
  "& .log-row": {
    display: "flex",
    gap: 1.5,
    pl: 1.25,
    borderLeft: "2px solid",
  },
  "& .log-raw": themeSx(unparsedTheme),
  "& .log-error": themeSx(rowTheme.error),
  "& .log-warn": themeSx(rowTheme.warn),
  "& .log-info": themeSx(rowTheme.info),
  "& .log-trace": themeSx(rowTheme.trace),
  "& .log-debug": themeSx(rowTheme.debug),
  "& .log-row:hover": { bgcolor: colors.accent.primaryStrong },
  "& .log-time": {
    flexShrink: 0,
    color: colors.text.disabled,
    userSelect: "none",
  },
  "& .log-message": { flex: 1, minWidth: 0 },
};

function trimTime(time: string): string {
  return time.replace(/(\.\d{3})\d*$/, "$1");
}

export const LogRow = memo(({ line }: { line: ParsedLogLine }) => (
  <div className={`log-row log-${line.level ?? "raw"}`}>
    {line.time && (
      <span
        className="log-time"
        title={`${line.date ?? ""} ${line.time}`.trim()}
      >
        {trimTime(line.time)}
      </span>
    )}
    <span className="log-message">{line.message}</span>
  </div>
));

LogRow.displayName = "LogRow";
