import { useState } from "react";
import { Box, Button, Stack, Typography } from "@mui/material";
import { DeleteForever as ClearIcon } from "@mui/icons-material";
import { CustomizeIcon } from "@b4.icons";
import { colors, fonts, radiusPx } from "@design";
import { B4Dialog } from "@common/B4Dialog";
import { useTranslation } from "react-i18next";
import type { Metrics } from "./Page";

interface HealthBannerProps {
  metrics: Metrics;
  connected: boolean;
  editing: boolean;
  onToggleEditing: () => void;
}

type HealthLevel = "healthy" | "degraded" | "critical";

function deriveHealth(metrics: Metrics, connected: boolean): HealthLevel {
  if (!connected) return "critical";
  if (
    metrics.nfqueue_status === "unknown" ||
    metrics.tables_status === "unknown"
  )
    return "degraded";
  const activeWorkers = metrics.worker_status.filter(
    (w) => w.status === "active",
  ).length;
  if (activeWorkers === 0 && metrics.worker_status.length > 0)
    return "critical";
  if (activeWorkers < metrics.worker_status.length) return "degraded";
  return "healthy";
}

const stateStyle: Record<
  HealthLevel,
  { bg: string; dot: string; name: string; state: string; glow: string }
> = {
  healthy: {
    bg: "rgba(102, 187, 106, 0.10)",
    dot: colors.state.success,
    name: "#9bd49d",
    state: "#cbe8cc",
    glow:
      "0 0 0 2px rgba(102, 187, 106, 0.18), 0 0 6px rgba(102, 187, 106, 0.55)",
  },
  degraded: {
    bg: "rgba(245, 173, 24, 0.10)",
    dot: colors.state.warning,
    name: "#ffd699",
    state: "#ffe0b2",
    glow:
      "0 0 0 2px rgba(255, 167, 38, 0.18), 0 0 6px rgba(255, 167, 38, 0.55)",
  },
  critical: {
    bg: "rgba(244, 67, 54, 0.10)",
    dot: colors.state.error,
    name: "#f8a5a0",
    state: "#fcc9c5",
    glow:
      "0 0 0 2px rgba(244, 67, 54, 0.18), 0 0 6px rgba(244, 67, 54, 0.55)",
  },
};

const HAIRLINE = "rgba(245, 173, 24, 0.08)";

const MetricCell = ({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) => (
  <Box
    sx={{
      display: "inline-flex",
      flexDirection: "column",
      justifyContent: "center",
      px: "18px",
      minWidth: 0,
      flexShrink: 0,
      whiteSpace: "nowrap",
      borderRight: `1px solid ${HAIRLINE}`,
    }}
  >
    <Typography
      component="span"
      sx={{
        fontSize: 9,
        letterSpacing: "0.16em",
        textTransform: "uppercase",
        color: colors.text.secondary,
        opacity: 0.85,
        lineHeight: 1,
      }}
    >
      {label}
    </Typography>
    <Typography
      component="span"
      sx={{
        fontSize: 13,
        color: colors.text.primary,
        fontWeight: 600,
        mt: "3px",
        lineHeight: 1,
        fontFeatureSettings: '"tnum"',
      }}
    >
      {children}
    </Typography>
  </Box>
);

const Mono = ({ children }: { children: React.ReactNode }) => (
  <Box
    component="span"
    sx={{ fontFamily: fonts.mono, fontSize: 12.5 }}
  >
    {children}
  </Box>
);

const actionCell = {
  display: "inline-flex",
  alignItems: "center",
  gap: "6px",
  px: "14px",
  flexShrink: 0,
  whiteSpace: "nowrap" as const,
  border: 0,
  borderLeft: `1px solid ${colors.border.default}`,
  cursor: "pointer",
  fontSize: 11,
  fontWeight: 600,
  letterSpacing: "0.08em",
  textTransform: "uppercase" as const,
  fontFamily: "inherit",
  transition: "background 150ms ease",
};

export const HealthBanner = ({
  metrics,
  connected,
  editing,
  onToggleEditing,
}: HealthBannerProps) => {
  const { t } = useTranslation();
  const [resetOpen, setResetOpen] = useState(false);
  const [resetting, setResetting] = useState(false);

  const health = deriveHealth(metrics, connected);
  const style = stateStyle[health];
  const healthLabel = t(
    `dashboard.health.${health === "healthy" ? "running" : health}`,
  );
  const activeWorkers = metrics.worker_status.filter(
    (w) => w.status === "active",
  ).length;
  const totalWorkers = metrics.worker_status.length;

  const handleReset = async () => {
    setResetOpen(false);
    setResetting(true);
    try {
      await fetch("/api/metrics/reset", { method: "POST" });
    } catch {
      // metrics will refresh via websocket
    } finally {
      setResetting(false);
    }
  };

  return (
    <>
      <Box
        sx={{
          display: "flex",
          alignItems: "stretch",
          height: 46,
          flexShrink: 0,
          mb: 1.5,
          bgcolor: colors.background.paper,
          border: `1px solid ${colors.border.default}`,
          borderRadius: `${radiusPx.md}px`,
          overflowX: "auto",
          overflowY: "hidden",
          scrollbarWidth: "none",
          "&::-webkit-scrollbar": { display: "none" },
        }}
      >
        <Box
          sx={{
            display: "inline-flex",
            alignItems: "center",
            gap: "8px",
            pl: "12px",
            pr: "14px",
            flexShrink: 0,
            whiteSpace: "nowrap",
            bgcolor: style.bg,
            borderRight: `1px solid ${colors.border.default}`,
          }}
        >
          <Box
            sx={{
              width: 7,
              height: 7,
              borderRadius: "50%",
              bgcolor: style.dot,
              boxShadow: style.glow,
              flexShrink: 0,
            }}
          />
          <Box
            sx={{
              display: "flex",
              flexDirection: "column",
              lineHeight: 1,
              gap: "3px",
            }}
          >
            <Typography
              component="span"
              sx={{
                fontSize: 11,
                fontWeight: 700,
                letterSpacing: "0.16em",
                textTransform: "uppercase",
                color: style.name,
              }}
            >
              B4
            </Typography>
            <Typography
              component="span"
              sx={{ fontSize: 12, color: style.state, fontWeight: 500 }}
            >
              {healthLabel}
            </Typography>
          </Box>
        </Box>

        <MetricCell label={t("dashboard.health.nfqueue")}>
          {metrics.nfqueue_status}
        </MetricCell>
        <MetricCell label={t("dashboard.health.firewall")}>
          <Mono>{metrics.tables_status}</Mono>
        </MetricCell>
        <MetricCell label={t("dashboard.health.workers")}>
          {activeWorkers}
          <Box
            component="span"
            sx={{ color: colors.text.secondary, fontWeight: 500 }}
          >
            {" / "}
            {totalWorkers}
          </Box>
        </MetricCell>
        <MetricCell label={t("dashboard.health.uptime")}>
          <Mono>{metrics.uptime}</Mono>
        </MetricCell>

        <Box sx={{ flex: "1 1 0", minWidth: 0, borderRight: `1px solid ${HAIRLINE}` }} />

        <Box
          component="button"
          type="button"
          onClick={onToggleEditing}
          sx={{
            ...actionCell,
            bgcolor: editing ? "rgba(245, 173, 24, 0.16)" : "transparent",
            color: editing ? colors.secondary : colors.text.primary,
            "&:hover": {
              bgcolor: "rgba(245, 173, 24, 0.22)",
              color: colors.secondary,
            },
          }}
        >
          <CustomizeIcon
            sx={{
              fontSize: 13,
              color: editing ? colors.secondary : colors.text.primary,
              flexShrink: 0,
            }}
          />
          {editing
            ? t("dashboard.customize.done")
            : t("dashboard.customize.action")}
        </Box>

        <Box
          component="button"
          type="button"
          onClick={() => setResetOpen(true)}
          disabled={resetting}
          sx={{
            ...actionCell,
            color: colors.text.primary,
            bgcolor: "transparent",
            cursor: resetting ? "not-allowed" : "pointer",
            "&:hover": {
              bgcolor: "rgba(158, 28, 96, 0.18)",
              color: "#fff",
            },
            "&:disabled": {
              opacity: 0.5,
            },
          }}
        >
          <ClearIcon
            sx={{ fontSize: 13, color: colors.primary, flexShrink: 0 }}
          />
          {resetting
            ? t("dashboard.health.resetting")
            : t("dashboard.health.resetStats")}
        </Box>
      </Box>

      <B4Dialog
        open={resetOpen}
        onClose={() => setResetOpen(false)}
        title={t("dashboard.health.resetTitle")}
        actions={
          <Stack direction="row" spacing={1}>
            <Button
              onClick={() => setResetOpen(false)}
              sx={{ color: colors.text.secondary }}
            >
              {t("core.cancel")}
            </Button>
            <Button
              onClick={() => void handleReset()}
              variant="contained"
              color="warning"
            >
              {t("dashboard.health.reset")}
            </Button>
          </Stack>
        }
      >
        <Typography sx={{ color: colors.text.primary, mt: 1 }}>
          {t("dashboard.health.resetConfirm")}
        </Typography>
      </B4Dialog>
    </>
  );
};
