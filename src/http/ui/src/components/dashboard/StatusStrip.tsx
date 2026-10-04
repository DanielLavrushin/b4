import { memo, useId, useState, type ReactNode } from "react";
import {
  Box,
  IconButton,
  ListItemIcon,
  ListItemText,
  Menu,
  MenuItem,
  Tooltip,
  type SxProps,
  type Theme,
} from "@mui/material";
import { MoreVert as MoreIcon } from "@mui/icons-material";
import { useTranslation } from "react-i18next";
import { colors, radiusPx } from "@design";
import {
  CheckCircleIcon,
  CustomizeIcon,
  ErrorIcon,
  RestoreIcon,
  StopIcon,
  SyncIcon,
} from "@b4.icons";
import {
  SPARK_BUCKET_MS,
  SparkLine,
  formatByteSize,
  formatClock,
  formatClockRange,
  formatInteger,
} from "@common/charts";
import type { EngineState, RSSBucket } from "@models/metrics";
import { shallowEqual, useMetricsFrame } from "@/stores/useMetrics";
import { formatDateTime, formatSince, useStaleSince } from "./panels";
import { srOnlySx } from "./panels/styles";
import { ResetDialog } from "./ResetDialog";
import {
  STATE_TONE,
  effectiveState,
  engineName,
  formatAge,
  formatPercent,
  formatUptime,
  sharePercent,
  type StateTone,
} from "./statusFormat";

const WIDE = "@container (min-width: 600px)";
const STALE_OPACITY = 0.5;
const MIB = 1024 * 1024;
const SPARK_MIN_SPAN_MS = 3_600_000;
const EMPTY_RSS: readonly RSSBucket[] = [];

const TONE_COLOR: Record<StateTone, string> = {
  success: colors.state.success,
  info: colors.state.info,
  warning: colors.state.warning,
  error: colors.state.error,
};

const STATE_ICON: Record<EngineState, ReactNode> = {
  running: <CheckCircleIcon />,
  starting: <SyncIcon />,
  stopping: <StopIcon />,
  failed: <ErrorIcon />,
};

const labelSx = {
  display: "block",
  fontSize: 11,
  fontWeight: 500,
  lineHeight: 1.35,
  letterSpacing: "0.02em",
  color: colors.text.secondary,
  whiteSpace: "nowrap",
} satisfies SxProps<Theme>;

const valueSx = {
  display: "flex",
  alignItems: "center",
  flexWrap: "wrap",
  columnGap: "8px",
  rowGap: "2px",
  mt: "2px",
  fontSize: 14,
  fontWeight: 600,
  lineHeight: 1.35,
  color: colors.text.primary,
  fontVariantNumeric: "tabular-nums",
  minWidth: 0,
} satisfies SxProps<Theme>;

const detailSx = {
  display: "block",
  mt: "1px",
  fontSize: 12,
  fontWeight: 400,
  lineHeight: 1.4,
  color: colors.text.secondary,
  fontVariantNumeric: "tabular-nums",
} satisfies SxProps<Theme>;

const hintLineSx = {
  display: "block",
  fontSize: 12,
  lineHeight: 1.5,
} satisfies SxProps<Theme>;

interface CellProps {
  label: string;
  value: ReactNode;
  detail?: ReactNode;
  hint?: readonly string[];
  narrowRow?: number;
}

function Cell({ label, value, detail, hint, narrowRow }: CellProps) {
  const lines = hint?.filter(Boolean) ?? [];
  const body = (
    <Box
      data-status-cell=""
      sx={{
        minWidth: 0,
        ...(narrowRow ? { gridColumn: "1 / -1", order: narrowRow } : null),
        [WIDE]: { order: 0, flex: "0 1 auto" },
      }}
    >
      <Box component="span" sx={labelSx}>
        {label}
      </Box>
      <Box component="span" sx={valueSx}>
        {value}
      </Box>
      {detail}
      {lines.length > 0 && (
        <Box component="span" sx={srOnlySx}>
          {lines.join(" ")}
        </Box>
      )}
    </Box>
  );
  if (lines.length === 0) return body;
  return (
    <Tooltip
      placement="bottom-start"
      title={
        <Box component="span">
          {lines.map((line) => (
            <Box component="span" key={line} sx={hintLineSx}>
              {line}
            </Box>
          ))}
        </Box>
      }
    >
      {body}
    </Tooltip>
  );
}

function Detail({ children }: { children: ReactNode }) {
  return (
    <Box component="span" sx={detailSx}>
      {children}
    </Box>
  );
}

const StateCell = memo(function StateCell() {
  const { t } = useTranslation();
  const state = useMetricsFrame((f) =>
    effectiveState(f.engine.state, Boolean(f.engine_failure)),
  );
  if (state === undefined) return null;
  const color = TONE_COLOR[STATE_TONE[state]];
  return (
    <Box
      data-status-cell=""
      sx={{
        minWidth: 0,
        gridColumn: 1,
        gridRow: 1,
        [WIDE]: { gridColumn: "auto", gridRow: "auto", flex: "0 0 auto" },
      }}
    >
      <Box component="span" sx={labelSx}>
        {t("dashboard.status.state")}
      </Box>
      <Box component="span" sx={{ ...valueSx, columnGap: "6px" }}>
        <Box
          component="span"
          aria-hidden
          sx={{ display: "inline-flex", color, "& svg": { fontSize: 18 } }}
        >
          {STATE_ICON[state]}
        </Box>
        <Box component="span" sx={{ color }}>
          {t(`dashboard.status.states.${state}`)}
        </Box>
      </Box>
    </Box>
  );
});

const EngineCell = memo(function EngineCell() {
  const { t, i18n } = useTranslation();
  const engine = useMetricsFrame(
    (f) => ({ mode: f.engine.mode, threads: f.engine.threads }),
    shallowEqual,
  );
  if (!engine) return null;
  const name = engineName(engine.mode);
  const threads = engine.threads > 0 ? engine.threads : 0;
  const value =
    threads > 0
      ? t("dashboard.status.engineValue", {
          engine: name,
          threads: formatInteger(threads, i18n.language),
        })
      : name;
  return (
    <Cell
      label={t("dashboard.status.engine")}
      value={value}
      hint={
        threads > 0
          ? [
              t("dashboard.status.engineHint", {
                engine: name,
                count: threads,
                value: formatInteger(threads, i18n.language),
              }),
            ]
          : undefined
      }
    />
  );
});

interface FirewallView {
  backend: string;
  monitored: boolean;
  ageMs: number;
  interval: number;
  restores: number;
  lastRestore: number;
  now: number;
}

const FirewallCell = memo(function FirewallCell() {
  const { t, i18n } = useTranslation();
  const locale = i18n.language;
  const view = useMetricsFrame(
    (f): FirewallView => ({
      backend: f.engine.firewall,
      monitored: f.rules.monitored,
      ageMs:
        f.rules.last_check > 0
          ? Math.floor(Math.max(0, f.now - f.rules.last_check) / 1000) * 1000
          : -1,
      interval: f.rules.interval_s,
      restores: f.rules.restores,
      lastRestore: f.rules.last_restore,
      now: Math.floor(f.now / 60_000) * 60_000,
    }),
    shallowEqual,
  );
  if (!view) return null;
  const label = t("dashboard.status.firewall");
  if (view.backend === "external") {
    return (
      <Cell
        label={label}
        narrowRow={1}
        value={t("dashboard.status.firewallExternal")}
        hint={[t("dashboard.status.firewallExternalHint")]}
      />
    );
  }
  const backend = view.backend || "-";
  let checked: string;
  if (!view.monitored) {
    checked = t("dashboard.status.firewallNotMonitored");
  } else if (view.ageMs < 0) {
    checked = t("dashboard.status.firewallNotChecked");
  } else {
    checked = t("dashboard.status.firewallChecked", {
      ago: formatAge(view.ageMs, t),
    });
  }
  const restoredText =
    view.restores > 0
      ? view.lastRestore > 0
        ? t("dashboard.status.firewallRestored", {
            count: view.restores,
            value: formatInteger(view.restores, locale),
            time: formatSince(view.lastRestore, view.now, locale),
          })
        : t("dashboard.status.firewallRestoredPlain", {
            count: view.restores,
            value: formatInteger(view.restores, locale),
          })
      : "";
  const hint = view.monitored
    ? [
        view.interval > 0
          ? t("dashboard.status.firewallHint", {
              seconds: formatInteger(view.interval, locale),
            })
          : t("dashboard.status.firewallHintPlain"),
        view.restores > 0 ? t("dashboard.status.firewallRestoredHint") : "",
      ]
    : [t("dashboard.status.firewallNotMonitoredHint")];
  return (
    <Cell
      label={label}
      value={backend}
      narrowRow={1}
      detail={
        <>
          <Detail>{checked}</Detail>
          {restoredText && <Detail>{restoredText}</Detail>}
        </>
      }
      hint={hint}
    />
  );
});

const UptimeCell = memo(function UptimeCell() {
  const { t, i18n } = useTranslation();
  const view = useMetricsFrame(
    (f) => {
      const uptime = f.uptime_s >= 60 ? Math.floor(f.uptime_s / 60) * 60 : f.uptime_s;
      const started = Math.floor((f.now - f.uptime_s * 1000) / 60_000) * 60_000;
      return { uptime, started, statsSince: f.stats_since };
    },
    shallowEqual,
  );
  if (!view) return null;
  const locale = i18n.language;
  const counted =
    view.statsSince > 0 && Math.abs(view.statsSince - view.started) > 60_000;
  return (
    <Cell
      label={t("dashboard.status.uptime")}
      value={formatUptime(view.uptime, t)}
      hint={[
        t("dashboard.status.uptimeHint", {
          time: formatDateTime(view.started, locale, false),
        }),
        counted
          ? t("dashboard.status.statsSinceHint", {
              time: formatDateTime(view.statsSince, locale),
            })
          : "",
      ]}
    />
  );
});

interface RamView {
  rss: number;
  total: number;
  history: readonly RSSBucket[];
  now: number;
}

function rssRange(history: readonly RSSBucket[], rss: number) {
  let peak = rss > 0 ? rss : 0;
  for (const bucket of history) {
    if (Number.isFinite(bucket.max) && bucket.max > peak) peak = bucket.max;
  }
  return { from: history.length > 0 ? history[0].t : 0, peak };
}

const RamCell = memo(function RamCell() {
  const { t, i18n } = useTranslation();
  const locale = i18n.language;
  const view = useMetricsFrame(
    (f): RamView => ({
      rss: f.process.rss_bytes,
      total: f.process.mem_total_bytes,
      history: f.rss_history,
      now: f.now,
    }),
    shallowEqual,
  );
  if (!view) return null;
  const label = t("dashboard.status.ram");
  if (!(view.rss > 0)) {
    return (
      <Cell
        label={label}
        value={t("dashboard.status.notAvailable")}
        hint={[t("dashboard.status.ramUnknownHint")]}
      />
    );
  }
  const share = sharePercent(view.rss, view.total);
  const { from, peak } = rssRange(view.history ?? EMPTY_RSS, view.rss);
  const size = formatByteSize(view.rss, locale);
  const hint = [
    t("dashboard.status.ramHint", {
      range: from > 0 ? formatClockRange(from, view.now, locale) : formatClock(view.now, locale),
      peak: formatByteSize(peak, locale),
      now: size,
      total:
        view.total > 0
          ? formatByteSize(view.total, locale)
          : t("dashboard.status.notAvailable"),
    }),
  ];
  return (
    <Cell
      label={label}
      value={size}
      detail={
        share !== null ? (
          <Detail>
            {t("dashboard.status.ramShare", {
              value: formatPercent(share, locale),
            })}
          </Detail>
        ) : undefined
      }
      hint={hint}
    />
  );
});

const CpuCell = memo(function CpuCell() {
  const { t, i18n } = useTranslation();
  const locale = i18n.language;
  const view = useMetricsFrame(
    (f) => ({
      percent: f.process.cpu_percent,
      peak: f.process.cpu_peak_percent,
      peakAt: f.process.cpu_peak_at,
      cores: f.process.cpu_cores,
    }),
    shallowEqual,
  );
  if (!view) return null;
  const cores = view.cores > 0 ? view.cores : 1;
  const hint = [
    t("dashboard.status.cpuHint", {
      count: cores,
      value: formatInteger(cores, locale),
    }),
    view.peakAt > 0
      ? t("dashboard.status.cpuPeak", {
          time: formatClock(view.peakAt, locale),
          peak: formatPercent(view.peak, locale),
          core: formatPercent(view.peak * cores, locale),
        })
      : "",
  ];
  return (
    <Cell
      label={t("dashboard.status.cpu")}
      value={formatPercent(view.percent, locale)}
      hint={hint}
    />
  );
});

interface RamTrendView {
  rss: number;
  history: readonly RSSBucket[];
  now: number;
}

const RamTrendCell = memo(function RamTrendCell() {
  const { t, i18n } = useTranslation();
  const locale = i18n.language;
  const view = useMetricsFrame(
    (f): RamTrendView => ({
      rss: f.process.rss_bytes,
      history: f.rss_history,
      now: f.now,
    }),
    shallowEqual,
  );
  if (!view || !(view.rss > 0)) return null;
  const history = view.history ?? EMPTY_RSS;
  const from = history.length > 0 ? history[0].t : 0;
  if (!(from > 0) || view.now - from < SPARK_MIN_SPAN_MS) return null;
  const now = view.now;
  return (
    <Box
      data-status-cell=""
      sx={{
        minWidth: 0,
        gridColumn: "1 / -1",
        order: 3,
        [WIDE]: { order: 0, flex: "1 1 120px", minWidth: 120 },
      }}
    >
      <Box component="span" sx={labelSx}>
        {t("dashboard.status.ramTrendLabel")}
      </Box>
      <Box sx={{ mt: "4px" }}>
        <SparkLine
          buckets={history}
          valueKey="max"
          now={now}
          unit={MIB}
          width="fill"
          height={30}
          area
          ariaLabel={t("dashboard.status.ramTrend")}
          readout={(bucket) =>
            t("dashboard.status.ramTrendPoint", {
              range: formatClockRange(
                bucket.t,
                Math.min(bucket.t + SPARK_BUCKET_MS, now),
                locale,
              ),
              value: formatByteSize(bucket.max, locale),
            })
          }
        />
      </Box>
    </Box>
  );
});

interface StatusStripProps {
  editing: boolean;
  onToggleEditing: () => void;
}

export const StatusStrip = memo(function StatusStrip({
  editing,
  onToggleEditing,
}: StatusStripProps) {
  const { t } = useTranslation();
  const menuId = useId();
  const buttonId = useId();
  const [anchor, setAnchor] = useState<HTMLElement | null>(null);
  const [resetOpen, setResetOpen] = useState(false);
  const staleSince = useStaleSince();
  const open = anchor !== null;

  return (
    <Box
      component="section"
      aria-label={t("dashboard.status.label")}
      sx={{ containerType: "inline-size", mb: 1.5 }}
    >
      <Box
        sx={{
          display: "grid",
          gridTemplateColumns: "repeat(2, minmax(0, 1fr))",
          alignItems: "start",
          columnGap: "16px",
          rowGap: "12px",
          px: "14px",
          py: "10px",
          bgcolor: colors.background.paper,
          border: `1px solid ${colors.border.default}`,
          borderRadius: `${radiusPx.md}px`,
          "& [data-status-cell]": {
            opacity: staleSince > 0 ? STALE_OPACITY : 1,
          },
          [WIDE]: {
            position: "relative",
            display: "flex",
            flexWrap: "wrap",
            alignItems: "flex-start",
            columnGap: "28px",
            rowGap: "10px",
            pl: "16px",
            pr: "52px",
          },
        }}
      >
        <StateCell />
        <EngineCell />
        <FirewallCell />
        <UptimeCell />
        <RamCell />
        <CpuCell />
        <RamTrendCell />
        <Box
          sx={{
            gridColumn: 2,
            gridRow: 1,
            justifySelf: "end",
            alignSelf: "start",
            mr: "-6px",
            [WIDE]: { position: "absolute", top: "10px", right: "8px", mr: 0 },
          }}
        >
          <Tooltip title={t("dashboard.status.menu")}>
            <IconButton
              id={buttonId}
              size="small"
              aria-label={t("dashboard.status.menu")}
              aria-haspopup="menu"
              aria-controls={open ? menuId : undefined}
              aria-expanded={open ? "true" : undefined}
              onClick={(event) => setAnchor(event.currentTarget)}
              sx={{ color: colors.text.secondary }}
            >
              <MoreIcon />
            </IconButton>
          </Tooltip>
        </Box>
      </Box>
      <Menu
        id={menuId}
        anchorEl={anchor}
        open={open}
        onClose={() => setAnchor(null)}
        slotProps={{ list: { "aria-labelledby": buttonId } }}
        anchorOrigin={{ vertical: "bottom", horizontal: "right" }}
        transformOrigin={{ vertical: "top", horizontal: "right" }}
      >
        <MenuItem
          onClick={() => {
            setAnchor(null);
            onToggleEditing();
          }}
        >
          <ListItemIcon>
            <CustomizeIcon fontSize="small" />
          </ListItemIcon>
          <ListItemText>
            {editing
              ? t("dashboard.customize.done")
              : t("dashboard.customize.action")}
          </ListItemText>
        </MenuItem>
        <MenuItem
          onClick={() => {
            setAnchor(null);
            setResetOpen(true);
          }}
        >
          <ListItemIcon>
            <RestoreIcon fontSize="small" />
          </ListItemIcon>
          <ListItemText>{t("dashboard.status.resetCounters")}</ListItemText>
        </MenuItem>
      </Menu>
      <ResetDialog open={resetOpen} onClose={() => setResetOpen(false)} />
    </Box>
  );
});
