import { useEffect, useRef, useState } from "react";
import { Box } from "@mui/material";
import { colors } from "@design";
import { formatBytes, formatNumber } from "@utils";
import { useTranslation } from "react-i18next";
import { DashboardPanel } from "./DashboardPanel";
import { StatCard } from "./StatCard";
import { SimpleLineChart } from "./SimpleLineChart";
import type { Metrics } from "./Page";

interface RuntimeHealthProps {
  metrics: Metrics;
}

const TREND_POINTS = 60;
const CHART_HEIGHT = 88;

export const RuntimeHealth = ({ metrics }: RuntimeHealthProps) => {
  const { t } = useTranslation();
  const mem = metrics.memory_usage;

  const seqRef = useRef(0);
  const [trend, setTrend] = useState<{ timestamp: number; value: number }[]>(
    [],
  );

  useEffect(() => {
    const goroutines = mem.goroutines;
    setTrend((prev) => {
      const next = [
        ...prev,
        { timestamp: seqRef.current++, value: goroutines },
      ];
      return next.length > TREND_POINTS
        ? next.slice(next.length - TREND_POINTS)
        : next;
    });
  }, [mem.goroutines]);

  const offHeap = mem.rss > mem.heap_sys ? mem.rss - mem.heap_sys : 0;
  const hasTrend = trend.length > 1;

  return (
    <DashboardPanel eyebrow={t("dashboard.runtime.title")} divider>
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
              gridTemplateColumns: {
                xs: "repeat(2, 1fr)",
                sm: "repeat(4, 1fr)",
              },
              gridAutoRows: "1fr",
              mr: "-1px",
              mb: "-1px",
              "& > *": {
                borderRight: `1px solid ${colors.border.light}`,
                borderBottom: `1px solid ${colors.border.light}`,
              },
            }}
          >
            <StatCard
              label={t("dashboard.runtime.rss")}
              value={formatBytes(mem.rss)}
              sub={`${formatBytes(offHeap)} ${t("dashboard.runtime.offHeap")}`}
              tone="primary"
            />
            <StatCard
              label={t("dashboard.runtime.heap")}
              value={formatBytes(mem.heap_inuse)}
              sub={`${formatBytes(mem.heap_sys)} ${t("dashboard.runtime.reserved")}`}
              tone="secondary"
            />
            <StatCard
              label={t("dashboard.runtime.goroutines")}
              value={formatNumber(mem.goroutines)}
              sub={t("dashboard.runtime.goroutinesHint")}
              tone="secondary"
            />
            <StatCard
              label={t("dashboard.runtime.openFds")}
              value={formatNumber(mem.open_fds)}
              sub={`${t("dashboard.runtime.gc")} ${formatNumber(mem.num_gc)}`}
              tone="muted"
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
          {hasTrend ? (
            <SimpleLineChart
              data={trend}
              color={colors.secondary}
              height={CHART_HEIGHT}
            />
          ) : (
            <Box sx={{ height: CHART_HEIGHT }} />
          )}
        </Box>
      </Box>
    </DashboardPanel>
  );
};
