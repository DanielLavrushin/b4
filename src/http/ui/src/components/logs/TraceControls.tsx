import { Box, Button, Stack, Typography } from "@mui/material";
import { StartIcon, StopIcon, DownloadIcon } from "@b4.icons";
import { B4TooltipButton } from "@b4.elements";
import { colors, fonts } from "@design";
import { useTranslation } from "react-i18next";
import type { TraceEndReason } from "@api/trace";

function formatElapsed(seconds: number): string {
  const m = Math.floor(seconds / 60)
    .toString()
    .padStart(2, "0");
  const s = (seconds % 60).toString().padStart(2, "0");
  return `${m}:${s}`;
}

interface TraceControlsProps {
  tracing: boolean;
  traceBusy: boolean;
  traceKnown: boolean;
  traceLines: number;
  traceElapsed: number;
  traceMaxMinutes: number;
  downloadReady: boolean;
  traceEndReason?: TraceEndReason;
  onStart: () => void;
  onStop: () => void;
  onDownload: () => void;
}

const END_KEYS: Partial<Record<TraceEndReason, string>> = {
  time_limit: "logs.trace.endedTimeLimit",
  write_failed: "logs.trace.endedWriteFailed",
  shutdown: "logs.trace.endedRestart",
  interrupted: "logs.trace.endedInterrupted",
};

export function TraceControls({
  tracing,
  traceBusy,
  traceKnown,
  traceLines,
  traceElapsed,
  traceMaxMinutes,
  downloadReady,
  traceEndReason,
  onStart,
  onStop,
  onDownload,
}: TraceControlsProps) {
  const { t } = useTranslation();
  const endKey = traceEndReason && END_KEYS[traceEndReason];

  if (tracing) {
    return (
      <Stack direction="row" spacing={1.5} alignItems="center">
        <Stack
          direction="row"
          spacing={1}
          alignItems="center"
          sx={{
            px: 1.25,
            py: 0.5,
            borderRadius: 1,
            border: `1px solid ${colors.state.error}`,
            bgcolor: "rgba(244, 67, 54, 0.10)",
            fontFamily: fonts.mono,
            fontSize: 12,
            color: colors.text.primary,
            whiteSpace: "nowrap",
          }}
        >
          <Box
            sx={{
              width: 9,
              height: 9,
              borderRadius: "50%",
              bgcolor: colors.state.error,
              "@keyframes b4recpulse": {
                "0%, 100%": { opacity: 1 },
                "50%": { opacity: 0.25 },
              },
              animation: "b4recpulse 1.2s ease-in-out infinite",
            }}
          />
          <span>
            {t("logs.trace.recording")} {formatElapsed(traceElapsed)}
          </span>
          <span style={{ color: colors.text.disabled }}>
            · {t("core.lines", { count: traceLines })}
          </span>
        </Stack>
        <Button
          size="small"
          variant="contained"
          color="error"
          disabled={traceBusy}
          startIcon={<StopIcon />}
          onClick={onStop}
          sx={{ flexShrink: 0, whiteSpace: "nowrap" }}
        >
          {t("logs.trace.stop")}
        </Button>
      </Stack>
    );
  }

  return (
    <Stack
      direction="row"
      spacing={0.5}
      alignItems="center"
      flexWrap="wrap"
      useFlexGap
    >
      {downloadReady && endKey && (
        <Typography
          variant="caption"
          sx={{ color: colors.text.secondary, mr: 1 }}
        >
          {t(endKey, { minutes: traceMaxMinutes })}
        </Typography>
      )}
      <Button
        size="small"
        variant="contained"
        disabled={traceBusy || !traceKnown}
        startIcon={<StartIcon />}
        onClick={onStart}
        title={t("logs.trace.startHint")}
        sx={{ flexShrink: 0, whiteSpace: "nowrap" }}
      >
        {t("logs.trace.start")}
      </Button>
      {downloadReady && (
        <B4TooltipButton
          title={t("logs.trace.downloadLast")}
          onClick={onDownload}
          icon={<DownloadIcon />}
        />
      )}
    </Stack>
  );
}
