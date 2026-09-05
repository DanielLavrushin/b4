import { useEffect, useRef } from "react";
import { Box, Typography, Button } from "@mui/material";
import { ClearIcon, LogsIcon, CloseIcon } from "@b4.icons";
import { colors, typography } from "@design";
import { B4Dialog } from "@common/B4Dialog";
import { useTranslation } from "react-i18next";

interface DiscoveryLogLineProps {
  logs: string[];
  connected: boolean;
  onOpen: () => void;
}

export const DiscoveryLogLine = ({
  logs,
  connected,
  onOpen,
}: DiscoveryLogLineProps) => {
  const { t } = useTranslation();
  const last = logs.at(-1) ?? "";

  return (
    <Box
      sx={{
        display: "flex",
        alignItems: "center",
        gap: 1.5,
        px: 1.5,
        py: 1,
        border: `1px solid ${colors.border.light}`,
        borderRadius: 1.5,
        bgcolor: colors.background.dark,
        minWidth: 0,
      }}
    >
      <Box
        sx={{
          width: 9,
          height: 9,
          borderRadius: "50%",
          flexShrink: 0,
          bgcolor: connected ? colors.secondary : colors.text.disabled,
          boxShadow: connected ? `0 0 8px ${colors.secondary}` : "none",
        }}
      />
      <Typography
        noWrap
        sx={{
          ...typography.recipes.monoSmall,
          fontSize: typography.sizes.sm,
          color: last ? colors.text.primary : colors.text.disabled,
          flex: 1,
          minWidth: 0,
        }}
      >
        {last || t("discovery.logs.waiting")}
      </Typography>
      <Button
        size="small"
        startIcon={<LogsIcon />}
        onClick={onOpen}
        sx={{ textTransform: "none", flexShrink: 0 }}
      >
        {t("discovery.run.showLog")}
      </Button>
    </Box>
  );
};

interface DiscoveryLogDialogProps {
  open: boolean;
  logs: string[];
  onClose: () => void;
  onClear: () => void;
}

export const DiscoveryLogDialog = ({
  open,
  logs,
  onClose,
  onClear,
}: DiscoveryLogDialogProps) => {
  const { t } = useTranslation();
  const scrollRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (scrollRef.current && open) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [logs, open]);

  return (
    <B4Dialog
      title={t("discovery.logs.title")}
      icon={<LogsIcon />}
      open={open}
      onClose={onClose}
      fullWidth
      maxWidth="xl"
      actions={
        <>
          <Button onClick={onClear} startIcon={<ClearIcon />} size="small">
            {t("discovery.logs.clear")}
          </Button>
          <Box sx={{ flex: 1 }} />
          <Button
            onClick={onClose}
            variant="contained"
            startIcon={<CloseIcon />}
          >
            {t("core.close")}
          </Button>
        </>
      }
    >
      <div
        ref={scrollRef}
        style={{
          height: "60vh",
          overflowY: "auto",
          backgroundColor: colors.background.dark,
          fontFamily: "monospace",
          fontSize: 12,
          padding: 16,
        }}
      >
        {logs.length === 0 ? (
          <Typography sx={{ color: colors.text.disabled, fontStyle: "italic" }}>
            {t("discovery.logs.waiting")}
          </Typography>
        ) : (
          logs.map((line, i) => (
            <div
              key={`${i}-${line.slice(0, 24)}`}
              style={{
                color: getLogColor(line),
                whiteSpace: "pre-wrap",
                wordBreak: "break-word",
                lineHeight: 1.6,
              }}
            >
              {line}
            </div>
          ))
        )}
      </div>
    </B4Dialog>
  );
};

function getLogColor(line: string): string {
  const lower = line.toLowerCase();
  if (lower.includes("success") || line.includes("✓") || lower.includes("best"))
    return colors.secondary;
  if (lower.includes("failed") || line.includes("✗") || lower.includes("fail"))
    return colors.primary;
  if (lower.includes("phase")) return colors.text.secondary;
  return colors.text.primary;
}
