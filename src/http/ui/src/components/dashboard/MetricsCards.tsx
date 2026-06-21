import { Box } from "@mui/material";
import { StatCard } from "./StatCard";
import { colors } from "@design";
import { formatNumber } from "@utils";
import { useTranslation } from "react-i18next";
import type { Metrics } from "./Page";

interface MetricsCardsProps {
  metrics: Metrics;
}

export const MetricsCards = ({ metrics }: MetricsCardsProps) => {
  const { t } = useTranslation();
  const targetRate =
    metrics.total_connections > 0
      ? ((metrics.targeted_connections / metrics.total_connections) * 100).toFixed(1)
      : "0.0";

  const isIdle = metrics.rst_dropped === 0;

  return (
    <Box
      sx={{
        display: "flex",
        flexDirection: { xs: "row", lg: "column" },
        height: "100%",
        "& > *:not(:last-of-type)": {
          borderBottom: { lg: `1px solid ${colors.border.light}` },
          borderRight: { xs: `1px solid ${colors.border.light}`, lg: "none" },
        },
      }}
    >
      <StatCard
        label={t("dashboard.metrics.targeted")}
        value={formatNumber(metrics.targeted_connections)}
        sub={`${targetRate}% ${t("dashboard.metrics.ofTotal")}`}
        tone="secondary"
      />
      <StatCard
        label={t("dashboard.metrics.rstDropped")}
        value={formatNumber(metrics.rst_dropped)}
        sub={isIdle ? t("dashboard.metrics.idle") : undefined}
        tone={isIdle ? "muted" : "primary"}
      />
      <StatCard
        label={t("dashboard.metrics.packets")}
        value={formatNumber(metrics.packets_processed)}
        sub={`${metrics.current_pps.toFixed(1)} ${t("dashboard.metrics.pktPerSec")}`}
        tone="primary"
      />
    </Box>
  );
};
