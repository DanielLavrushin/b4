import { Box, Paper, Typography } from "@mui/material";
import { colors, fonts, radiusPx } from "@design";
import { SimpleLineChart } from "./SimpleLineChart";
import { MetricsCards } from "./MetricsCards";
import { useTranslation } from "react-i18next";
import type { Metrics } from "./Page";

interface LiveSignalProps {
  metrics: Metrics;
}

const CHART_HEIGHT = 140;

export const LiveSignal = ({ metrics }: LiveSignalProps) => {
  const { t } = useTranslation();
  const hasData = metrics.connection_rate.length > 0;

  return (
    <Paper
      variant="outlined"
      sx={{
        bgcolor: colors.background.paper,
        borderColor: colors.border.default,
        borderRadius: `${radiusPx.md}px`,
        overflow: "hidden",
        display: "flex",
        flexDirection: { xs: "column", lg: "row" },
      }}
    >
      <Box
        sx={{
          flex: 1,
          minWidth: 0,
          display: "flex",
          flexDirection: "column",
          p: "12px 14px 14px",
        }}
      >
        <Box
          sx={{
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
            gap: "12px",
            mb: 1,
          }}
        >
          <Typography
            variant="metricLabel"
            sx={{ color: colors.text.secondary, opacity: 0.8 }}
          >
            {t("dashboard.signal.title")}
          </Typography>
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
            <Box
              component="span"
              sx={{ color: colors.text.primary, fontWeight: 700 }}
            >
              {metrics.current_cps.toFixed(1)} {t("dashboard.signal.cps")}
            </Box>
          </Box>
        </Box>

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
            <Typography
              sx={{ fontSize: 11, color: colors.text.disabled }}
            >
              {t("dashboard.signal.idleHint")}
            </Typography>
          </Box>
        )}
      </Box>

      <Box
        sx={{
          width: { xs: "100%", lg: 300 },
          flexShrink: 0,
          borderTop: { xs: `1px solid ${colors.border.light}`, lg: "none" },
          borderLeft: { lg: `1px solid ${colors.border.light}` },
        }}
      >
        <MetricsCards metrics={metrics} />
      </Box>
    </Paper>
  );
};
