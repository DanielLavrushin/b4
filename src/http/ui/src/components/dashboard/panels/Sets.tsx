import { memo, useMemo, type ReactNode } from "react";
import { Box, Link, Tooltip } from "@mui/material";
import { Link as RouterLink } from "react-router";
import { Trans, useTranslation } from "react-i18next";
import { colors, fonts } from "@design";
import {
  CheckCircleIcon,
  ErrorIcon,
  HideIcon,
  InfoIcon,
  SyncIcon,
  TimerIcon,
  WarningIcon,
} from "@b4.icons";
import { formatInteger } from "@common/charts";
import { measureText } from "@common/charts/measure";
import type {
  SetActivity,
  SetKind,
  UpstreamAttention,
} from "@models/metrics";
import type { SetWatchState, SetWatchStatus } from "@models/watchdog";
import { useWatchdogSetStatuses } from "@hooks/useWatchdog";
import { useMetricsFrame } from "@/stores/useMetrics";
import { Ago } from "./Ago";
import { PanelCard } from "./PanelCard";
import { Tag } from "./Tag";
import { formatSince } from "./format";
import { emptySx, linkSx, listSx, numeric, rowSx, srOnlySx } from "./styles";

export type WatchLabel =
  | "healthy"
  | "failing"
  | "healing"
  | "gaveUp"
  | "cantCheck"
  | "notChecked"
  | "off";

export const WATCH_LABEL: Record<SetWatchState, WatchLabel> = {
  queued: "notChecked",
  healthy: "healthy",
  degraded: "failing",
  heal_queued: "healing",
  healing: "healing",
  cooldown: "failing",
  unverifiable: "cantCheck",
  gave_up: "gaveUp",
};

const WATCH_LABELS: readonly WatchLabel[] = [
  "healthy",
  "failing",
  "healing",
  "gaveUp",
  "cantCheck",
  "notChecked",
  "off",
];

const WATCH_VISUAL: Record<WatchLabel, { icon: ReactNode; color: string }> = {
  healthy: { icon: <CheckCircleIcon />, color: colors.state.success },
  failing: { icon: <WarningIcon />, color: colors.state.warning },
  healing: { icon: <SyncIcon />, color: colors.state.info },
  gaveUp: { icon: <ErrorIcon />, color: colors.state.error },
  cantCheck: { icon: <InfoIcon />, color: colors.text.secondary },
  notChecked: { icon: <TimerIcon />, color: colors.text.secondary },
  off: { icon: <HideIcon />, color: colors.text.secondary },
};

const KIND_KEY: Record<SetKind, string> = {
  bypass: "dashboard.sets.kind.bypass",
  route: "dashboard.sets.kind.route",
  proxy: "dashboard.sets.kind.proxy",
  block: "dashboard.sets.kind.block",
};

const EMPTY_SETS: readonly SetActivity[] = [];
const EMPTY_UPSTREAMS: readonly UpstreamAttention[] = [];

const CELL_FONT_PX = 12;
const TAG_FONT_PX = 11;
const BOLD_FACTOR = 1.1;
const TAG_CHROME_PX = 36;
const TEXT_SLACK_PX = 6;
const NAME_MIN_PX = 150;
const DETAIL_MIN_PX = 110;
const COLUMN_GAP_PX = 12;
const ROW_PAD_PX = 28;
const COUNT_SAMPLE = 88_888;
const AGO_SAMPLE_MINUTES = 59;

interface SetsLayout {
  wideQuery: string;
  columns: string;
  areas: string;
  watch: boolean;
}

export function watchLabelFor(
  status: SetWatchStatus | undefined,
  watchdogOn: boolean,
): WatchLabel {
  if (!watchdogOn) return "off";
  if (!status) return "notChecked";
  return WATCH_LABEL[status.status] ?? "notChecked";
}

function useSetsLayout(list: readonly SetActivity[]): SetsLayout {
  const { t, i18n } = useTranslation();
  const locale = i18n.language;
  const anyWatched = list.some((s) => s.watched);
  const anyNoMatch = list.some((s) => !(s.last_match > 0));
  return useMemo(() => {
    const measure = (text: string, px = CELL_FONT_PX) =>
      measureText(text, px, fonts.sans);
    const count =
      Math.ceil(
        measure(
          t("dashboard.sets.lastHour", {
            value: formatInteger(COUNT_SAMPLE, locale),
          }),
        ),
      ) + TEXT_SLACK_PX;
    const agoSample = t("dashboard.common.ago", {
      duration: t("charts.minutes", { n: AGO_SAMPLE_MINUTES }),
    });
    let last = Math.max(
      measure(t("dashboard.sets.lastMatch", { ago: agoSample })),
      measure(t("dashboard.sets.lastMatch", { ago: t("dashboard.common.justNow") })),
    );
    if (anyNoMatch) last = Math.max(last, measure(t("dashboard.sets.noMatch")));
    last = Math.ceil(last) + TEXT_SLACK_PX;
    let watch = 0;
    if (anyWatched) {
      for (const label of WATCH_LABELS) {
        watch = Math.max(
          watch,
          measure(t(`dashboard.sets.watch.${label}`), TAG_FONT_PX),
        );
      }
      watch = Math.ceil(watch * BOLD_FACTOR) + TAG_CHROME_PX;
    }
    const columns = [
      `minmax(${NAME_MIN_PX}px, 1.4fr)`,
      `${count}px`,
      `${last}px`,
      `minmax(${DETAIL_MIN_PX}px, 1fr)`,
    ];
    const areas = ["name", "count", "last", "detail"];
    if (watch > 0) {
      columns.push(`${watch}px`);
      areas.push("watch");
    }
    const minimum =
      ROW_PAD_PX +
      NAME_MIN_PX +
      count +
      last +
      DETAIL_MIN_PX +
      watch +
      COLUMN_GAP_PX * (columns.length - 1);
    return {
      wideQuery: `@container (min-width: ${minimum}px)`,
      columns: columns.join(" "),
      areas: `"${areas.join(" ")}"`,
      watch: watch > 0,
    };
  }, [t, locale, anyWatched, anyNoMatch]);
}

interface WatchChipProps {
  setId: string;
  status: SetWatchStatus | undefined;
  watchdogOn: boolean;
}

const WatchChip = memo(function WatchChip({
  setId,
  status,
  watchdogOn,
}: WatchChipProps) {
  const { t } = useTranslation();
  const label = watchLabelFor(status, watchdogOn);
  const visual = WATCH_VISUAL[label];
  const tip: string[] = [];
  if (!watchdogOn) {
    tip.push(t("dashboard.sets.watch.offHint"));
  } else if (status) {
    tip.push(
      t("dashboard.sets.watch.state", {
        state: t(`watchdog.setStatus.${status.status}`, {
          defaultValue: status.status,
        }),
      }),
    );
    if (status.reason) {
      tip.push(t(`watchdog.reason.${status.reason}`, { defaultValue: "" }));
    }
  } else {
    tip.push(t("dashboard.sets.watch.pendingHint"));
  }
  const to = watchdogOn
    ? `/sets/${encodeURIComponent(setId)}?tab=discovery`
    : "/watchdog";
  return (
    <Tooltip title={tip.filter(Boolean).join(" ")} describeChild>
      <Tag
        to={to}
        icon={visual.icon}
        iconColor={visual.color}
        label={t(`dashboard.sets.watch.${label}`)}
      />
    </Tooltip>
  );
});

function UpstreamState({ upstream }: { upstream: UpstreamAttention }) {
  const { t, i18n } = useTranslation();
  const tip = t(
    upstream.fail_open
      ? "dashboard.sets.upstreamTipOpen"
      : "dashboard.sets.upstreamTipClosed",
    {
      upstream: upstream.upstream,
      value: formatInteger(upstream.failures, i18n.language),
      time: formatSince(upstream.last_failure, Date.now(), i18n.language),
      error: upstream.last_error || "-",
    },
  );
  return (
    <Tooltip title={tip} describeChild>
      <Box
        component="span"
        sx={{
          display: "inline-flex",
          alignItems: "center",
          gap: "4px",
          color: colors.text.primary,
        }}
      >
        <Box
          component="span"
          aria-hidden
          sx={{
            display: "inline-flex",
            color: upstream.fail_open ? colors.state.warning : colors.state.error,
            "& svg": { fontSize: 14 },
          }}
        >
          {upstream.fail_open ? <WarningIcon /> : <ErrorIcon />}
        </Box>
        {t("dashboard.sets.upstreamDown")}
      </Box>
    </Tooltip>
  );
}

interface SetRowProps {
  set: SetActivity;
  watch: SetWatchStatus | undefined;
  watchdogOn: boolean;
  watchLoaded: boolean;
  upstream: UpstreamAttention | undefined;
  layout: SetsLayout;
}

const cellSx = {
  minWidth: 0,
  fontSize: CELL_FONT_PX,
  lineHeight: 1.45,
  color: colors.text.secondary,
} as const;

const SetRow = memo(function SetRow({
  set,
  watch,
  watchdogOn,
  watchLoaded,
  upstream,
  layout,
}: SetRowProps) {
  const { t, i18n } = useTranslation();
  const locale = i18n.language;
  const countTip = set.watched
    ? `${t("dashboard.sets.countHint")} ${t("dashboard.sets.countWatchedHint")}`
    : t("dashboard.sets.countHint");

  const detail: ReactNode[] = [];
  if (set.kind === "proxy" && set.open !== undefined) {
    detail.push(
      <Box key="open" component="span" sx={numeric}>
        {t("dashboard.sets.openNow", { value: formatInteger(set.open, locale) })}
      </Box>,
    );
  }
  if (upstream) detail.push(<UpstreamState key="up" upstream={upstream} />);
  if (set.kind === "block") {
    detail.push(
      <Box key="dns" component="span" sx={numeric}>
        {t("dashboard.sets.dnsBlocked", {
          count: set.dns_blocked,
          value: formatInteger(set.dns_blocked, locale),
        })}
      </Box>,
    );
  }

  return (
    <Box
      component="li"
      sx={{
        ...rowSx,
        display: "grid",
        alignItems: "center",
        columnGap: `${COLUMN_GAP_PX}px`,
        rowGap: "4px",
        gridTemplateColumns: "minmax(0, 1fr) auto",
        gridTemplateAreas: '"name watch" "meta meta"',
        [layout.wideQuery]: {
          gridTemplateColumns: layout.columns,
          gridTemplateAreas: layout.areas,
        },
      }}
    >
      <Box
        sx={{
          gridArea: "name",
          display: "flex",
          alignItems: "center",
          gap: "8px",
          minWidth: 0,
        }}
      >
        <Link
          component={RouterLink}
          underline="hover"
          to={`/sets/${encodeURIComponent(set.id)}`}
          title={set.name || set.id}
          sx={{
            minWidth: 0,
            overflow: "hidden",
            textOverflow: "ellipsis",
            whiteSpace: "nowrap",
            fontSize: 14,
            fontWeight: 600,
            color: colors.text.primary,
            textUnderlineOffset: "3px",
            "&:hover": { color: colors.text.primary },
            "&:focus-visible": {
              outline: `2px solid ${colors.border.strong}`,
              outlineOffset: "2px",
              borderRadius: "2px",
            },
          }}
        >
          {set.name || set.id}
        </Link>
        <Tag label={t(KIND_KEY[set.kind] ?? KIND_KEY.bypass)} />
      </Box>
      <Box
        sx={{
          gridArea: "meta",
          display: "flex",
          flexWrap: "wrap",
          alignItems: "center",
          columnGap: "14px",
          rowGap: "2px",
          minWidth: 0,
          [layout.wideQuery]: { display: "contents" },
        }}
      >
        <Box sx={{ ...cellSx, gridArea: "count" }}>
          <Tooltip title={countTip} describeChild>
            <Box component="span" sx={{ ...numeric, color: colors.text.primary }}>
              {t("dashboard.sets.lastHour", {
                value: formatInteger(set.conns_60m, locale),
              })}
            </Box>
          </Tooltip>
          {set.watched && (
            <Box component="span" sx={srOnlySx}>
              {` ${t("dashboard.sets.countWatchedHint")}`}
            </Box>
          )}
        </Box>
        <Box sx={{ ...cellSx, gridArea: "last" }}>
          {set.last_match > 0 ? (
            <Ago
              at={set.last_match}
              template={(ago) => t("dashboard.sets.lastMatch", { ago })}
            />
          ) : (
            <Box component="span">{t("dashboard.sets.noMatch")}</Box>
          )}
        </Box>
        {detail.length > 0 && (
          <Box
            sx={{
              ...cellSx,
              gridArea: "detail",
              display: "flex",
              flexWrap: "wrap",
              alignItems: "center",
              columnGap: "12px",
              rowGap: "2px",
            }}
          >
            {detail}
          </Box>
        )}
      </Box>
      {set.watched && watchLoaded && (
        <Box sx={{ gridArea: "watch", justifySelf: "end", minWidth: 0 }}>
          <WatchChip setId={set.id} status={watch} watchdogOn={watchdogOn} />
        </Box>
      )}
    </Box>
  );
});

function SetsPanelView() {
  const { t, i18n } = useTranslation();
  const sets = useMetricsFrame((f) => f.sets);
  const disabled = useMetricsFrame((f) => f.sets_disabled) ?? 0;
  const upstreams =
    useMetricsFrame((f) => f.attention.upstreams) ?? EMPTY_UPSTREAMS;
  const list = sets ?? EMPTY_SETS;
  const anyWatched = list.some((s) => s.watched);
  const anyBypass = list.some((s) => s.kind === "bypass");
  const watch = useWatchdogSetStatuses(anyWatched);
  const layout = useSetsLayout(list);

  const upstreamBySet = useMemo(() => {
    const map = new Map<string, UpstreamAttention>();
    for (const u of upstreams) {
      const known = map.get(u.set_id);
      if (!known || u.failures > known.failures) map.set(u.set_id, u);
    }
    return map;
  }, [upstreams]);

  const footer: ReactNode[] = [];
  if (disabled > 0) {
    footer.push(
      <Box key="disabled" component="span">
        <Link component={RouterLink} underline="hover" to="/sets" sx={linkSx}>
          {t("dashboard.sets.disabled", {
            count: disabled,
            value: formatInteger(disabled, i18n.language),
          })}
        </Link>
      </Box>,
    );
  }
  if (list.length > 0 && !anyWatched && anyBypass) {
    footer.push(
      <Box key="watchdog" component="span">
        <Trans
          i18nKey="dashboard.sets.noWatchdog"
          components={{
            a: (
              <Link
                component={RouterLink}
                underline="hover"
                to="/watchdog"
                sx={linkSx}
              />
            ),
          }}
        />
      </Box>,
    );
  }

  let body: ReactNode;
  if (list.length === 0) {
    body = (
      <Box sx={emptySx}>
        {disabled > 0 ? (
          <Box component="p" sx={{ m: 0 }}>
            {t("dashboard.sets.allDisabled")}
          </Box>
        ) : (
          <>
            <Box component="p" sx={{ m: 0, mb: "8px" }}>
              {t("dashboard.sets.empty")}
            </Box>
            <Box sx={{ display: "flex", flexWrap: "wrap", gap: "6px 16px" }}>
              <Link component={RouterLink} underline="hover" to="/sets/new" sx={linkSx}>
                {t("dashboard.sets.createSet")}
              </Link>
              <Link component={RouterLink} underline="hover" to="/discovery" sx={linkSx}>
                {t("dashboard.sets.openDiscovery")}
              </Link>
            </Box>
          </>
        )}
      </Box>
    );
  } else {
    body = (
      <Box component="ul" sx={listSx}>
        {list.map((set) => (
          <SetRow
            key={set.id}
            set={set}
            watch={watch.byId.get(set.id)}
            watchdogOn={watch.enabled}
            watchLoaded={watch.loaded}
            upstream={upstreamBySet.get(set.id)}
            layout={layout}
          />
        ))}
      </Box>
    );
  }

  return (
    <PanelCard
      title={t("dashboard.sets.title")}
      subtitle={t("dashboard.sets.subtitle")}
      waiting={sets === undefined}
      footer={footer.length > 0 ? footer : undefined}
    >
      {body}
    </PanelCard>
  );
}

export const SetsPanel = memo(SetsPanelView);
