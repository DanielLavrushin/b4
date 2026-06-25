import { Box } from "@mui/material";
import { colors } from "@design";
import { LogLevel, ParsedLogLine } from "./parse";

const rowTheme: Record<
  LogLevel,
  { border: string; tint: string; text: string }
> = {
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

const unparsedTheme = {
  border: "rgba(245, 173, 24, 0.20)",
  tint: "transparent",
  text: colors.text.primary,
};

function trimTime(time: string): string {
  return time.replace(/(\.\d{3})\d*$/, "$1");
}

export function LogRow({ line }: { line: ParsedLogLine }) {
  const theme = line.level ? rowTheme[line.level] : unparsedTheme;
  return (
    <Box
      sx={{
        display: "flex",
        gap: 1.5,
        pl: 1.25,
        borderLeft: `2px solid ${theme.border}`,
        bgcolor: theme.tint,
        color: theme.text,
        "&:hover": { bgcolor: colors.accent.primaryStrong },
      }}
    >
      {line.time && (
        <Box
          component="span"
          title={`${line.date ?? ""} ${line.time}`.trim()}
          sx={{
            flexShrink: 0,
            color: colors.text.disabled,
            userSelect: "none",
          }}
        >
          {trimTime(line.time)}
        </Box>
      )}
      <Box component="span" sx={{ flex: 1, minWidth: 0 }}>
        {line.message}
      </Box>
    </Box>
  );
}
