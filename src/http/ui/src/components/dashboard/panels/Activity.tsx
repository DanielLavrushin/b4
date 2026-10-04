import { memo, useCallback, useMemo, useRef, useState } from "react";
import { Box, Link, ToggleButton, ToggleButtonGroup } from "@mui/material";
import { Link as RouterLink } from "react-router";
import { useTranslation } from "react-i18next";
import { colors } from "@design";
import {
  TimeBars,
  formatClock,
  formatCount,
  formatInteger,
  type TimeBarsSeries,
} from "@common/charts";
import {
  MINUTE_BUCKETS,
  MINUTE_MS,
  TEN_MINUTE_BUCKETS,
  TEN_MINUTE_MS,
  type ActivityBucket,
} from "@models/metrics";
import { useMetricsFrame } from "@/stores/useMetrics";
import { PanelCard } from "./PanelCard";
import { formatSince, sumBuckets } from "./format";
import { readChoice, writeChoice } from "./storage";
import { PANEL_PAD_X, linkSx, numeric } from "./styles";

type ActivityKey = "in_sets" | "not_in_set";
export type ActivityWindow = "1h" | "24h";

const WINDOWS: readonly ActivityWindow[] = ["1h", "24h"];
export const ACTIVITY_WINDOW_KEY = "b4_dashboard_activity_window";
const CHART_HEIGHT = 184;
const EMPTY_BUCKETS: readonly ActivityBucket[] = [];

const toggleSx = {
  "& .MuiToggleButton-root": { px: "10px", py: "3px", fontSize: 11 },
} as const;

const START_JITTER_MS = 2_000;

function useStableStart(now: number, uptime: number): number | undefined {
  const ref = useRef<number | undefined>(undefined);
  if (!(now > 0) || !(uptime >= 0)) return ref.current;
  const start = now - uptime * 1000;
  const known = ref.current;
  if (known === undefined || Math.abs(start - known) > START_JITTER_MS) {
    ref.current = start;
  }
  return ref.current;
}

function ActivityPanelView() {
  const { t, i18n } = useTranslation();
  const locale = i18n.language;
  const [windowKey, setWindowKey] = useState<ActivityWindow>(() =>
    readChoice(ACTIVITY_WINDOW_KEY, WINDOWS, "1h"),
  );
  const [table, setTable] = useState(false);
  const hour = windowKey === "1h";

  const buckets = useMetricsFrame((f) =>
    hour ? f.activity.minute : f.activity.ten_minute,
  );
  const now = useMetricsFrame((f) => f.now) ?? 0;
  const uptime = useMetricsFrame((f) => f.uptime_s) ?? 0;
  const rstDropped = useMetricsFrame((f) => f.totals.rst_dropped) ?? 0;
  const statsSince = useMetricsFrame((f) => f.stats_since) ?? 0;

  const series = useMemo(
    (): TimeBarsSeries<ActivityKey>[] => [
      {
        key: "in_sets",
        label: t("dashboard.activity.inSets"),
        color: colors.secondary,
      },
      {
        key: "not_in_set",
        label: t("dashboard.activity.notInSet"),
        color: colors.chart.rest,
      },
    ],
    [t],
  );

  const rows = buckets ?? EMPTY_BUCKETS;
  const totals = useMemo(() => sumBuckets(rows), [rows]);

  const formatTenMinute = useCallback(
    (raw: number, _count: number, spanMs: number) => {
      const total = formatCount(raw, locale);
      if (!(spanMs >= MINUTE_MS)) return total;
      return t("dashboard.activity.tenMinuteValue", {
        total,
        perMinute: formatCount(raw / (spanMs / MINUTE_MS), locale, 1),
      });
    },
    [t, locale],
  );

  const chooseWindow = (_: unknown, value: ActivityWindow | null) => {
    if (!value || value === windowKey) return;
    setWindowKey(value);
    writeChoice(ACTIVITY_WINDOW_KEY, value);
  };

  const startedAt = useStableStart(now, uptime);
  const windowLabel = hour
    ? t("dashboard.activity.lastHour")
    : t("dashboard.activity.last24h");

  const actions = (
    <>
      <Box sx={{ display: "flex", flexWrap: "wrap", gap: "4px 14px" }}>
        <Link component={RouterLink} underline="hover" to="/traffic" sx={linkSx}>
          {t("dashboard.activity.liveTraffic")}
        </Link>
        <Link component={RouterLink} underline="hover" to="/traffic?unmatched=1" sx={linkSx}>
          {t("dashboard.activity.unmatched")}
        </Link>
      </Box>
      <Box sx={{ display: "flex", gap: "8px" }}>
        <ToggleButtonGroup
          size="small"
          exclusive
          value={windowKey}
          onChange={chooseWindow}
          aria-label={t("dashboard.activity.window")}
          sx={toggleSx}
        >
          <ToggleButton value="1h">{t("dashboard.activity.window1h")}</ToggleButton>
          <ToggleButton value="24h">{t("dashboard.activity.window24h")}</ToggleButton>
        </ToggleButtonGroup>
        <ToggleButtonGroup size="small" sx={toggleSx}>
          <ToggleButton
            value="table"
            selected={table}
            onChange={() => setTable((value) => !value)}
          >
            {t("dashboard.activity.table")}
          </ToggleButton>
        </ToggleButtonGroup>
      </Box>
    </>
  );

  const footer =
    rstDropped > 0 ? (
      <Box component="span">
        {t("dashboard.activity.rstDropped", {
          value: formatInteger(rstDropped, locale),
          time: formatSince(statsSince, now, locale),
        })}
      </Box>
    ) : undefined;

  return (
    <PanelCard
      title={t("dashboard.activity.title")}
      subtitle={t("dashboard.activity.subtitle")}
      actions={actions}
      footer={footer}
      waiting={buckets === undefined}
    >
      <Box
        sx={{
          display: "flex",
          flexWrap: "wrap",
          alignItems: "center",
          gap: "4px 16px",
          px: PANEL_PAD_X,
          pb: "2px",
          fontSize: 12,
          lineHeight: 1.5,
          color: colors.text.secondary,
        }}
      >
        <Box component="span">{windowLabel}</Box>
        {series.map((s) => {
          const value = totals[s.key];
          return (
            <Box
              key={s.key}
              component="span"
              sx={{ display: "inline-flex", alignItems: "center", gap: "6px" }}
            >
              <Box
                component="span"
                aria-hidden
                sx={{
                  width: 10,
                  height: 10,
                  borderRadius: "2px",
                  bgcolor: s.color,
                  flexShrink: 0,
                }}
              />
              <Box
                component="span"
                title={formatInteger(value, locale)}
                sx={{ ...numeric, fontWeight: 700, color: colors.text.primary }}
              >
                {formatCount(value, locale)}
              </Box>
              <Box component="span">{s.label}</Box>
            </Box>
          );
        })}
      </Box>
      <TimeBars
        buckets={rows}
        series={series}
        slots={hour ? MINUTE_BUCKETS : TEN_MINUTE_BUCKETS}
        bucketMs={hour ? MINUTE_MS : TEN_MINUTE_MS}
        now={now}
        summaryLabel={windowLabel}
        emptyText={
          hour
            ? t("dashboard.activity.emptyHour")
            : t("dashboard.activity.emptyDay")
        }
        height={CHART_HEIGHT}
        divisor={hour ? 1 : 10}
        formatValue={hour ? undefined : formatTenMinute}
        startedAt={startedAt}
        startLabel={
          startedAt
            ? t("dashboard.activity.started", {
                time: formatClock(startedAt, locale),
              })
            : undefined
        }
        table={table}
      />
    </PanelCard>
  );
}

export const ActivityPanel = memo(ActivityPanelView);
