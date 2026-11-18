import { Fragment, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Box, CircularProgress } from "@mui/material";
import CheckCircleIcon from "@mui/icons-material/CheckCircle";
import CancelIcon from "@mui/icons-material/Cancel";
import HourglassEmptyIcon from "@mui/icons-material/HourglassEmpty";
import RadioButtonUncheckedIcon from "@mui/icons-material/RadioButtonUnchecked";
import NetworkPingIcon from "@mui/icons-material/NetworkPing";
import { colors, fonts } from "@design";
import { B4RunBar } from "@b4.elements";
import type { TransportProbeResult } from "@api/mtproto";
import { PanelCard } from "@components/dashboard/panels/PanelCard";
import {
  PANEL_PAD_X,
  listSx,
  numeric,
  rowSx,
} from "@components/dashboard/panels/styles";

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

const LINE = "18px";

const iconSx = {
  inlineSize: LINE,
  blockSize: LINE,
  flex: "none",
  display: "inline-flex",
  alignItems: "center",
  justifyContent: "center",
  "& svg": { fontSize: 16 },
} as const;

const NARROW = "@container (max-width: 480px)";

const wordSx = {
  flexShrink: 0,
  fontSize: 12,
  lineHeight: LINE,
  whiteSpace: "nowrap",
  textAlign: "right",
} as const;

interface ProbeRowItemProps {
  row: ProbeRow;
  state: RowState;
}

const ProbeRowItem = ({ row, state }: ProbeRowItemProps) => {
  const { t } = useTranslation();
  const result = row.result;

  let icon: ReactNode;
  let right: ReactNode;
  let stage = "";
  switch (state) {
    case "ok":
      icon = (
        <CheckCircleIcon
          titleAccess={t("settings.MTProto.testRowOk")}
          sx={{ color: colors.state.success }}
        />
      );
      right = (
        <Box
          component="span"
          sx={{
            ...numeric,
            flexShrink: 0,
            fontSize: 13,
            fontWeight: 700,
            lineHeight: LINE,
            whiteSpace: "nowrap",
            color: colors.text.primary,
          }}
        >
          {t("settings.MTProto.testLatencyMs", { ms: result?.latency_ms ?? 0 })}
        </Box>
      );
      break;
    case "failed":
      icon = (
        <CancelIcon
          titleAccess={t("settings.MTProto.testRowFailed")}
          sx={{ color: colors.state.error }}
        />
      );
      stage = result?.stage
        ? t(`settings.MTProto.testStage_${result.stage}`, {
            defaultValue: result.stage,
          })
        : t("settings.MTProto.testRowFailed");
      right = (
        <Box
          component="span"
          sx={{
            ...wordSx,
            flexShrink: 1,
            maxInlineSize: "45%",
            whiteSpace: "normal",
            color: colors.state.error,
            [NARROW]: { display: "none" },
          }}
        >
          {stage}
        </Box>
      );
      break;
    case "testing":
      icon = (
        <CircularProgress
          size={14}
          thickness={5}
          disableShrink
          aria-hidden
          sx={{ color: colors.secondary }}
        />
      );
      right = (
        <Box component="span" sx={{ ...wordSx, color: colors.secondary }}>
          {t("settings.MTProto.testRowTesting")}
        </Box>
      );
      break;
    case "queued":
      icon = <RadioButtonUncheckedIcon sx={{ color: colors.text.disabled }} />;
      right = (
        <Box component="span" sx={{ ...wordSx, color: colors.text.disabled }}>
          {t("settings.MTProto.testRowQueued")}
        </Box>
      );
      break;
    default:
      icon = <HourglassEmptyIcon sx={{ color: colors.text.disabled }} />;
      right = (
        <Box component="span" sx={{ ...wordSx, color: colors.text.disabled }}>
          {t("settings.MTProto.testStage_untested")}
        </Box>
      );
  }

  const detail = state === "failed" ? result?.error : undefined;

  return (
    <Box
      component="li"
      sx={{ ...rowSx, display: "flex", alignItems: "flex-start", gap: "12px" }}
    >
      <Box sx={iconSx}>{icon}</Box>
      <Box sx={{ flex: "1 1 auto", minInlineSize: 0 }}>
        <Box
          component="span"
          sx={{
            display: "block",
            fontFamily: fonts.mono,
            fontSize: 12,
            lineHeight: LINE,
            color: colors.text.primary,
            overflowWrap: "anywhere",
          }}
        >
          {breakableRoute(row.transport)}
        </Box>
        {stage && (
          <Box
            component="span"
            sx={{
              display: "none",
              mt: "2px",
              fontSize: 12,
              lineHeight: 1.4,
              color: colors.state.error,
              [NARROW]: { display: "block" },
            }}
          >
            {stage}
          </Box>
        )}
        {detail && (
          <Box
            component="span"
            sx={{
              display: "block",
              mt: "2px",
              fontSize: 12,
              lineHeight: 1.4,
              color: colors.text.secondary,
              overflowWrap: "anywhere",
            }}
          >
            {detail}
          </Box>
        )}
      </Box>
      {right}
    </Box>
  );
};

interface MTProtoProbeResultsProps {
  dc: number;
  rows: ProbeRow[];
  running: boolean;
}

export const MTProtoProbeResults = ({
  dc,
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
  const summary = t("settings.MTProto.testSummary", { ok, total });

  return (
    <PanelCard
      title={t("settings.MTProto.testResultsTitle", { dc })}
      subtitle={
        <Box component="span" role="status">
          {suffix ? `${summary} · ${suffix}` : summary}
        </Box>
      }
      icon={<NetworkPingIcon />}
      live={false}
    >
      {running && (
        <Box sx={{ px: PANEL_PAD_X, pb: "12px" }}>
          <B4RunBar
            value={total ? ((total - left) / total) * 100 : 0}
            label={t("settings.MTProto.testResultsLabel")}
          />
        </Box>
      )}
      <Box
        component="ul"
        aria-label={t("settings.MTProto.testResultsLabel")}
        sx={listSx}
      >
        {rows.map((row, i) => (
          <ProbeRowItem key={i} row={row} state={states[i]} />
        ))}
      </Box>
    </PanelCard>
  );
};
