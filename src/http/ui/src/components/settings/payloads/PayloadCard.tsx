import { useTranslation } from "react-i18next";
import {
  Box,
  Chip,
  IconButton,
  Paper,
  Stack,
  Tooltip,
  Typography,
} from "@mui/material";
import { ClearIcon, CopyIcon, DownloadIcon } from "@b4.icons";
import { colors, radius } from "@design";
import type {
  PayloadFacts,
  PayloadOrigin,
  PayloadProtocol,
  PayloadWarning,
} from "@models/capture";
import { bytesLabel, kindLabel } from "./facts";
import { PayloadWarnings } from "./PayloadWarnings";

interface PayloadCardProps {
  title: string;
  protocol: PayloadProtocol;
  size: number;
  facts?: PayloadFacts | null;
  origin?: PayloadOrigin;
  timestamp?: string;
  warnings?: PayloadWarning[];
  usedBy?: string[];
  onViewHex?: () => void;
  onDownload?: () => void;
  onDelete?: () => void;
}

const present = (parts: (string | null | undefined | false)[]) =>
  parts.filter((part): part is string => !!part);

export const PayloadCard = ({
  title,
  protocol,
  size,
  facts,
  origin,
  timestamp,
  warnings = [],
  usedBy = [],
  onViewHex,
  onDownload,
  onDelete,
}: PayloadCardProps) => {
  const { t } = useTranslation();

  const summary = present([
    facts && kindLabel(t, facts),
    bytesLabel(t, size),
    facts?.fits_one_packet === true && t("settings.Capture.fitsOnePacket"),
    facts?.fits_one_packet === false && t("settings.Capture.overOnePacket"),
  ]);
  const details = present([
    facts?.sni && t("settings.Capture.sni", { sni: facts.sni }),
    !!facts?.extensions &&
      t("settings.Capture.extensions", { count: facts.extensions }),
    facts?.ech && t("settings.Capture.ech"),
  ]);
  const provenance = present([
    origin && t(`settings.Capture.origins.${origin}`),
    timestamp && new Date(timestamp).toLocaleString(),
  ]);

  return (
    <Paper
      elevation={0}
      sx={{
        p: 2,
        height: "100%",
        display: "flex",
        flexDirection: "column",
        gap: 1,
        border: `1px solid ${colors.border.default}`,
        borderRadius: radius.md,
        transition: "all 0.2s ease",
        "&:hover": {
          borderColor: colors.secondary,
          transform: "translateY(-2px)",
          boxShadow: `0 4px 12px ${colors.accent.primary}40`,
        },
      }}
    >
      <Stack direction="row" justifyContent="space-between" alignItems="center">
        <Typography
          variant="subtitle1"
          fontWeight={600}
          sx={{
            minWidth: 0,
            flex: 1,
            overflow: "hidden",
            textOverflow: "ellipsis",
            whiteSpace: "nowrap",
          }}
        >
          {title}
        </Typography>
        <Chip
          label={protocol.toUpperCase()}
          size="small"
          sx={{
            ml: 1,
            height: 20,
            fontSize: "0.65rem",
            fontWeight: 700,
            letterSpacing: "0.05em",
            bgcolor:
              protocol === "quic"
                ? colors.accent.secondary
                : `${colors.state.info}26`,
            color: protocol === "quic" ? colors.secondary : colors.state.info,
            border: `1px solid ${
              protocol === "quic"
                ? colors.border.strong
                : `${colors.state.info}66`
            }`,
            "& .MuiChip-label": { px: 0.75 },
          }}
        />
      </Stack>

      <Box sx={{ overflowWrap: "anywhere" }}>
        <Typography variant="body2">{summary.join(" · ")}</Typography>
        {details.length > 0 && (
          <Typography variant="caption" color="text.secondary" component="div">
            {details.join(" · ")}
          </Typography>
        )}
        {provenance.length > 0 && (
          <Typography variant="caption" color="text.secondary" component="div">
            {provenance.join(" · ")}
          </Typography>
        )}
      </Box>

      <PayloadWarnings warnings={warnings} protocol={protocol} facts={facts} />

      {usedBy.length > 0 && (
        <Typography
          variant="caption"
          color="text.secondary"
          sx={{ overflowWrap: "anywhere" }}
        >
          {t("settings.Capture.usedBy", { sets: usedBy.join(", ") })}
        </Typography>
      )}

      <Box sx={{ flex: 1 }} />

      <Stack
        direction="row"
        spacing={1}
        sx={{ pt: 1, borderTop: `1px solid ${colors.border.light}` }}
      >
        {onViewHex && (
          <Tooltip title={t("settings.Capture.viewHex")}>
            <IconButton size="small" onClick={onViewHex}>
              <CopyIcon fontSize="small" />
            </IconButton>
          </Tooltip>
        )}
        {onDownload && (
          <Tooltip title={t("settings.Capture.downloadBin")}>
            <IconButton size="small" onClick={onDownload}>
              <DownloadIcon fontSize="small" />
            </IconButton>
          </Tooltip>
        )}
        <Box sx={{ flex: 1 }} />
        {onDelete && (
          <Tooltip title={t("core.delete")}>
            <IconButton size="small" onClick={onDelete}>
              <ClearIcon fontSize="small" />
            </IconButton>
          </Tooltip>
        )}
      </Stack>
    </Paper>
  );
};
