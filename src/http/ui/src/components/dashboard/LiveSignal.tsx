import { Box, Typography } from "@mui/material";
import { colors, fonts } from "@design";
import { SimpleLineChart } from "./SimpleLineChart";
import { StatCard } from "./StatCard";
import { DashboardPanel } from "./DashboardPanel";
import { formatNumber } from "@utils";
import { useTranslation } from "react-i18next";
import type { Metrics } from "./Page";

interface LiveSignalProps {
  metrics: Metrics;
}

const CHART_HEIGHT = 88;

export const LiveSignal = ({ metrics }: LiveSignalProps) => {
  const { t } = useTranslation();
  const hasData = metrics.connection_rate.length > 0;

  const targetRate =
    metrics.total_connections > 0
      ? (
          (metrics.targeted_connections / metrics.total_connections) *
          100
        ).toFixed(1)
      : "0.0";
  const isIdle = metrics.rst_dropped === 0;

  const legend = (
    <Box
      sx={{
        display: "flex",
        alignItems: "center",
        gap: "10px",
        fontFamily: fonts.mono,
        fontSize: 11,
        color: colors.text.secondary,
      }}
    >
      <Box
        component="span"
        sx={{ display: "inline-flex", alignItems: "center", gap: "6px" }}
      >
        <Box
          component="span"
          sx={{ width: 8, height: 2, bgcolor: colors.secondary }}
        />
        {t("dashboard.connectionRateLegend")}
      </Box>
      <Box component="span" sx={{ color: colors.text.primary, fontWeight: 700 }}>
        {metrics.current_cps.toFixed(1)} {t("dashboard.signal.cps")}
      </Box>
    </Box>
  );

  return (
    <DashboardPanel
      eyebrow={t("dashboard.signal.title")}
      right={legend}
      divider
    >
      <Box
        sx={{
          display: "flex",
          flexDirection: { xs: "column", lg: "row" },
          alignItems: "stretch",
        }}
      >
        <Box
          sx={{
            width: { lg: 720 },
            flexShrink: 0,
            overflow: "hidden",
            display: "flex",
          }}
        >
          <Box
            sx={{
              flex: 1,
              display: "grid",
              gridTemplateColumns: "repeat(3, 1fr)",
              gridAutoRows: "1fr",
              mr: "-1px",
              "& > *": {
                borderRight: `1px solid ${colors.border.light}`,
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
        </Box>

        <Box
          sx={{
            flex: 1,
            minWidth: 0,
            display: "flex",
            flexDirection: "column",
            justifyContent: "center",
            pt: { xs: 1.5, lg: "6px" },
            pb: { lg: "6px" },
            pl: { lg: 1.5 },
            borderLeft: { lg: `1px solid ${colors.border.light}` },
          }}
        >
          {hasData ? (
            <SimpleLineChart
              data={metrics.connection_rate}
              color={colors.secondary}
              height={CHART_HEIGHT}
            />
          ) : (
            <Box
              sx={{
                height: CHART_HEIGHT,
                display: "flex",
                flexDirection: "column",
                alignItems: "center",
                justifyContent: "center",
                gap: "6px",
                position: "relative",
                "&::before": {
                  content: '""',
                  position: "absolute",
                  left: 0,
                  right: 0,
                  bottom: "33%",
                  height: "1px",
                  bgcolor: colors.border.light,
                },
              }}
            >
              <Typography
                sx={{
                  fontFamily: fonts.mono,
                  fontSize: 12,
                  color: colors.text.secondary,
                  letterSpacing: "0.06em",
                  textTransform: "uppercase",
                }}
              >
                {t("dashboard.signal.idle")}
              </Typography>
              <Typography sx={{ fontSize: 11, color: colors.text.disabled }}>
                {t("dashboard.signal.idleHint")}
              </Typography>
            </Box>
          )}
        </Box>
      </Box>
    </DashboardPanel>
  );
};
