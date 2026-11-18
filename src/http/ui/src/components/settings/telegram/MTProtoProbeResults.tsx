import { Fragment } from "react";
import { useTranslation } from "react-i18next";
import {
  Box,
  CircularProgress,
  LinearProgress,
  Stack,
  Typography,
} from "@mui/material";
import CheckCircleIcon from "@mui/icons-material/CheckCircle";
import CancelIcon from "@mui/icons-material/Cancel";
import HourglassEmptyIcon from "@mui/icons-material/HourglassEmpty";
import RadioButtonUncheckedIcon from "@mui/icons-material/RadioButtonUnchecked";
import { fonts } from "@design";
import type { TransportProbeResult } from "@api/mtproto";

export type ProbeRow = {
  transport: string;
  status: "queued" | "testing" | "done";
  result?: TransportProbeResult;
};

type RowState = "ok" | "failed" | "testing" | "queued" | "untested";

const rowState = (row: ProbeRow, running: boolean): RowState => {
  if (row.status === "done" && row.result) {
    if (row.result.ok) return "ok";
    return row.result.stage === "untested" ? "untested" : "failed";
  }
  if (!running) return "untested";
  return row.status === "testing" ? "testing" : "queued";
};

const breakableRoute = (route: string) =>
  route.split(/([.@?])/).map((part, i) =>
    i % 2 === 1 ? (
      <Fragment key={i}>
        {part}
        <wbr />
      </Fragment>
    ) : (
      part
    ),
  );

const iconBoxSx = {
  inlineSize: 20,
  blockSize: 20,
  flex: "none",
  display: "inline-flex",
  alignItems: "center",
  justifyContent: "center",
} as const;

interface ProbeRowItemProps {
  row: ProbeRow;
  state: RowState;
}

const ProbeRowItem = ({ row, state }: ProbeRowItemProps) => {
  const { t } = useTranslation();
  const result = row.result;

  let icon;
  let label: string;
  let labelColor = "text.disabled";
  switch (state) {
    case "ok":
      icon = (
        <CheckCircleIcon
          fontSize="small"
          color="success"
          titleAccess={t("settings.MTProto.testRowOk")}
        />
      );
      label = t("settings.MTProto.testLatencyMs", {
        ms: result?.latency_ms ?? 0,
      });
      labelColor = "text.secondary";
      break;
    case "failed":
      icon = (
        <CancelIcon
          fontSize="small"
          color="error"
          titleAccess={t("settings.MTProto.testRowFailed")}
        />
      );
      label = result?.stage
        ? t(`settings.MTProto.testStage_${result.stage}`, {
            defaultValue: result.stage,
          })
        : t("settings.MTProto.testRowFailed");
      labelColor = "error.main";
      break;
    case "testing":
      icon = (
        <CircularProgress
          size={16}
          thickness={4.5}
          aria-hidden
          sx={{ color: "text.secondary" }}
        />
      );
      label = t("settings.MTProto.testRowTesting");
      labelColor = "text.secondary";
      break;
    case "queued":
      icon = (
        <RadioButtonUncheckedIcon
          fontSize="small"
          sx={{ color: "text.disabled" }}
        />
      );
      label = t("settings.MTProto.testRowQueued");
      break;
    default:
      icon = (
        <HourglassEmptyIcon fontSize="small" sx={{ color: "text.disabled" }} />
      );
      label = t("settings.MTProto.testStage_untested");
  }

  const detail = state === "failed" ? result?.error : undefined;

  return (
    <Box
      component="li"
      sx={{ display: "flex", alignItems: "flex-start", gap: 1 }}
    >
      <Box sx={iconBoxSx}>{icon}</Box>
      <Box sx={{ flex: "1 1 0", minInlineSize: 0 }}>
        <Box sx={{ display: "flex", alignItems: "baseline", gap: 1 }}>
          <Typography
            variant="body2"
            sx={{
              flex: "1 1 auto",
              minInlineSize: 0,
              fontFamily: fonts.mono,
              fontSize: { xs: "0.75rem", sm: "0.875rem" },
              overflowWrap: "anywhere",
            }}
          >
            {breakableRoute(row.transport)}
          </Typography>
          <Typography
            variant="body2"
            sx={{
              flex: state === "failed" ? "0 1 auto" : "none",
              maxInlineSize: state === "failed" ? "45%" : "none",
              whiteSpace: state === "failed" ? "normal" : "nowrap",
              textAlign: "end",
              color: labelColor,
              fontVariantNumeric: "tabular-nums",
            }}
          >
            {label}
          </Typography>
        </Box>
        {detail && (
          <Typography
            variant="caption"
            component="p"
            sx={{
              color: "text.secondary",
              fontFamily: fonts.mono,
              overflowWrap: "anywhere",
            }}
          >
            {detail}
          </Typography>
        )}
      </Box>
    </Box>
  );
};

interface MTProtoProbeResultsProps {
  rows: ProbeRow[];
  running: boolean;
}

export const MTProtoProbeResults = ({
  rows,
  running,
}: MTProtoProbeResultsProps) => {
  const { t } = useTranslation();
  const states = rows.map((row) => rowState(row, running));
  const total = rows.length;
  const ok = states.filter((s) => s === "ok").length;
  const left = states.filter((s) => s === "testing" || s === "queued").length;
  const untested = states.filter((s) => s === "untested").length;

  let suffix = "";
  if (running) {
    suffix = t("settings.MTProto.testSummaryLeft", { n: left });
  } else if (untested > 0) {
    suffix = t("settings.MTProto.testSummaryUntested", { n: untested });
  }

  return (
    <Stack spacing={1}>
      <Box
        role="status"
        sx={{
          display: "flex",
          flexWrap: "wrap",
          alignItems: "baseline",
          columnGap: 1,
        }}
      >
        <Typography variant="body2" sx={{ fontWeight: 600 }}>
          {t("settings.MTProto.testSummary", { ok, total })}
        </Typography>
        {suffix && (
          <Typography variant="body2" sx={{ color: "text.secondary" }}>
            {`· ${suffix}`}
          </Typography>
        )}
      </Box>
      {running && (
        <LinearProgress
          variant="determinate"
          value={total ? ((total - left) / total) * 100 : 0}
          aria-label={t("settings.MTProto.testResultsLabel")}
          sx={{ blockSize: 4, borderRadius: 2 }}
        />
      )}
      <Box
        component="ul"
        aria-label={t("settings.MTProto.testResultsLabel")}
        sx={{
          listStyle: "none",
          m: 0,
          p: 0,
          display: "flex",
          flexDirection: "column",
          gap: 0.75,
        }}
      >
        {rows.map((row, i) => (
          <ProbeRowItem key={i} row={row} state={states[i]} />
        ))}
      </Box>
    </Stack>
  );
};
