import { Chip, Stack } from "@mui/material";
import { FilterIcon } from "@b4.icons";
import { colors, fonts } from "@design";
import { LogLevel, LOG_LEVELS } from "./parse";

const levelColor: Record<LogLevel, string> = {
  error: colors.state.error,
  warn: colors.state.warning,
  info: colors.state.info,
  trace: colors.text.secondary,
  debug: colors.text.secondary,
};

interface LevelFilterBarProps {
  enabledLevels: Set<LogLevel>;
  levelCounts: Record<LogLevel, number>;
  onToggle: (level: LogLevel) => void;
}

export function LevelFilterBar({
  enabledLevels,
  levelCounts,
  onToggle,
}: LevelFilterBarProps) {
  return (
    <Stack
      direction="row"
      spacing={1}
      alignItems="center"
      sx={{ mt: 1.5, flexWrap: "wrap", rowGap: 1 }}
    >
      <FilterIcon sx={{ fontSize: 16, color: colors.text.disabled, mr: 0.5 }} />
      {LOG_LEVELS.map((level) => {
        const active = enabledLevels.has(level);
        const color = levelColor[level];
        return (
          <Chip
            key={level}
            size="small"
            label={`${level.toUpperCase()} ${levelCounts[level]}`}
            onClick={() => onToggle(level)}
            sx={{
              fontFamily: fonts.mono,
              fontSize: 11,
              letterSpacing: "0.04em",
              cursor: "pointer",
              color: active ? color : colors.text.disabled,
              borderColor: active ? color : colors.border.light,
              bgcolor: active ? colors.background.hover : "transparent",
              border: "1px solid",
              opacity: active ? 1 : 0.6,
              "&:hover": { bgcolor: colors.background.hover },
            }}
            variant="outlined"
          />
        );
      })}
    </Stack>
  );
}
