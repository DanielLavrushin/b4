import { memo, useCallback, useId, useMemo, useState } from "react";
import { Box, Button, IconButton, Link, Tooltip } from "@mui/material";
import { Link as RouterLink } from "react-router";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { colors, fonts, radiusPx } from "@design";
import { CloseIcon, ErrorIcon, WarningIcon } from "@b4.icons";
import { systemApi } from "@api/settings";
import { mtprotoApi } from "@api/mtproto";
import { telegramBridgeKey } from "@hooks/useTelegramBridge";
import { useWatchdogSetStatuses } from "@hooks/useWatchdog";
import type { B4Event, SetActivity } from "@models/metrics";
import { shallowEqual, useMetricsFrame, useServerNow } from "@/stores/useMetrics";
import { formatDateTime, formatSince } from "./panels";
import { ShowMore, visibleRows } from "./panels/ShowMore";
import { linkSx, srOnlySx } from "./panels/styles";
import { writeChoice } from "./panels/storage";
import {
  IPV6_DISMISS,
  QUIET_AFTER_MS,
  buildAttentionItems,
  type AttentionItem,
  type AttentionLevel,
  type QuietView,
  type ThreadView,
  type WatchedSet,
} from "./attentionItems";

const IPV6_STORAGE_KEY = "b4_ipv6_bypass_dismissed";
const DISMISSED_STORAGE_KEY = "b4_dashboard_dismissed";
const DISMISSED_CAP = 50;
const SYSTEM_INFO_STALE_MS = 5 * 60 * 1000;
const BRIDGE_STALE_MS = 30 * 1000;
const NOW_TICK_MS = 60_000;
const COLLAPSED_ITEMS = 6;

const EMPTY_WATCHED: readonly WatchedSet[] = [];
const EMPTY_EVENTS: readonly B4Event[] = [];
const NOT_QUIET: QuietView = { since: 0, fromStart: false };

const LEVEL_COLOR: Record<AttentionLevel, string> = {
  error: colors.state.error,
  warning: colors.state.warning,
};

function readDismissed(): string[] {
  try {
    const raw = window.localStorage.getItem(DISMISSED_STORAGE_KEY);
    const parsed: unknown = raw ? JSON.parse(raw) : [];
    return Array.isArray(parsed)
      ? parsed.filter((entry): entry is string => typeof entry === "string")
      : [];
  } catch {
    return [];
  }
}

function readIpv6Dismissed(): boolean {
  try {
    return window.localStorage.getItem(IPV6_STORAGE_KEY) === "true";
  } catch {
    return false;
  }
}

function useDismissals() {
  const [keys, setKeys] = useState<string[]>(readDismissed);
  const [ipv6, setIpv6] = useState<boolean>(readIpv6Dismissed);
  const dismissed = useMemo(() => {
    const set = new Set(keys);
    if (ipv6) set.add(IPV6_DISMISS);
    return set;
  }, [keys, ipv6]);
  const dismiss = useCallback((key: string) => {
    if (key === IPV6_DISMISS) {
      writeChoice(IPV6_STORAGE_KEY, "true");
      setIpv6(true);
      return;
    }
    setKeys((prev) => {
      const next = [...prev.filter((entry) => entry !== key), key].slice(
        -DISMISSED_CAP,
      );
      writeChoice(DISMISSED_STORAGE_KEY, JSON.stringify(next));
      return next;
    });
  }, []);
  return { dismissed, dismiss };
}

const selectWatched = (frame: { sets: SetActivity[] }): readonly WatchedSet[] => {
  const watched = frame.sets.filter((set) => set.watched);
  if (watched.length === 0) return EMPTY_WATCHED;
  return watched.map((set) => ({ id: set.id, name: set.name }));
};

const sameWatched = (a: readonly WatchedSet[], b: readonly WatchedSet[]) =>
  a.length === b.length &&
  a.every((entry, i) => entry.id === b[i].id && entry.name === b[i].name);

function useSystemView() {
  const query = useQuery({
    queryKey: ["system", "info"],
    queryFn: () => systemApi.info(),
    staleTime: SYSTEM_INFO_STALE_MS,
    retry: false,
  });
  const data = query.data;
  return useMemo(
    () =>
      data
        ? {
            hostHasIPv6: data.host_has_global_ipv6 === true,
            ipv6BypassesSets: data.ipv6_bypasses_sets === true,
          }
        : undefined,
    [data],
  );
}

function useBridgeStatus() {
  const query = useQuery({
    queryKey: telegramBridgeKey,
    queryFn: () => mtprotoApi.telegramBridge(),
    staleTime: BRIDGE_STALE_MS,
    retry: false,
  });
  return query.data;
}

interface RowProps {
  item: AttentionItem;
  now: number;
  onDismiss: (key: string) => void;
  onRestart: () => void;
}

const AttentionRow = memo(function AttentionRow({
  item,
  now,
  onDismiss,
  onRestart,
}: RowProps) {
  const { t, i18n } = useTranslation();
  const locale = i18n.language;
  const color = LEVEL_COLOR[item.level];
  const action = item.action;
  return (
    <Box
      component="li"
      sx={{
        display: "flex",
        alignItems: "flex-start",
        gap: "10px",
        px: "14px",
        py: "9px",
        borderTop: `1px solid ${colors.border.light}`,
        boxShadow: `inset 3px 0 0 ${color}`,
      }}
    >
      <Box
        component="span"
        aria-hidden
        sx={{ display: "inline-flex", color, mt: "1px", "& svg": { fontSize: 18 } }}
      >
        {item.level === "error" ? <ErrorIcon /> : <WarningIcon />}
      </Box>
      <Box
        sx={{
          flex: 1,
          minWidth: 0,
          display: "flex",
          flexWrap: "wrap",
          alignItems: "flex-start",
          columnGap: "16px",
          rowGap: "6px",
        }}
      >
        <Box sx={{ flex: "1 1 300px", minWidth: 0 }}>
          <Box sx={{ fontSize: 13, lineHeight: 1.5, color: colors.text.primary }}>
            <Box component="span" sx={srOnlySx}>
              {t(`dashboard.events.level.${item.level}`)}:{" "}
            </Box>
            {item.text}
          </Box>
          {(item.detail || item.time) && (
            <Box
              sx={{
                display: "flex",
                flexWrap: "wrap",
                columnGap: "10px",
                rowGap: "2px",
                mt: "2px",
                fontSize: 12,
                lineHeight: 1.45,
                color: colors.text.secondary,
              }}
            >
              {item.time ? (
                <Tooltip title={formatDateTime(item.time, locale)} describeChild>
                  <Box
                    component="time"
                    dateTime={new Date(item.time).toISOString()}
                    sx={{ whiteSpace: "nowrap", fontVariantNumeric: "tabular-nums" }}
                  >
                    {formatSince(item.time, now, locale)}
                  </Box>
                </Tooltip>
              ) : null}
              {item.detail ? (
                <Box
                  component="span"
                  sx={{
                    minWidth: 0,
                    overflowWrap: "anywhere",
                    ...(item.detailIsText
                      ? null
                      : { fontFamily: fonts.mono, fontSize: 11.5 }),
                  }}
                >
                  {item.detail}
                </Box>
              ) : null}
            </Box>
          )}
        </Box>
        {(action || item.dismiss) && (
          <Box
            sx={{
              display: "flex",
              alignItems: "center",
              gap: "6px",
              ml: "auto",
              minHeight: 22,
            }}
          >
            {action?.kind === "link" && (
              <Link component={RouterLink} to={action.to} underline="hover" sx={linkSx}>
                {action.label}
              </Link>
            )}
            {action?.kind === "restart" && (
              <Button
                size="small"
                variant="outlined"
                onClick={onRestart}
                sx={{ py: "1px", px: "10px", fontSize: 12, textTransform: "none" }}
              >
                {action.label}
              </Button>
            )}
            {item.dismiss && (
              <Tooltip title={t("dashboard.attention.dismiss")}>
                <IconButton
                  size="small"
                  aria-label={t("dashboard.attention.dismissItem", { item: item.text })}
                  onClick={() => onDismiss(item.dismiss ?? "")}
                  sx={{ color: colors.text.secondary, p: "3px" }}
                >
                  <CloseIcon sx={{ fontSize: 16 }} />
                </IconButton>
              </Tooltip>
            )}
          </Box>
        )}
      </Box>
    </Box>
  );
});

interface AttentionProps {
  onRestart: () => void;
}

export const Attention = memo(function Attention({ onRestart }: AttentionProps) {
  const { t, i18n } = useTranslation();
  const locale = i18n.language;
  const headingId = useId();
  const listId = useId();
  const [expanded, setExpanded] = useState(false);
  const watched = useMetricsFrame(selectWatched, sameWatched) ?? EMPTY_WATCHED;
  const attention = useMetricsFrame((f) => f.attention);
  const mode = useMetricsFrame((f) => f.engine.mode);
  const errors = useMetricsFrame((f) => f.events?.errors) ?? EMPTY_EVENTS;
  const threads = useMetricsFrame(
    (f): ThreadView => ({
      os_threads: f.process.os_threads,
      thread_warn: f.process.thread_warn,
      thread_limit: f.process.thread_limit,
    }),
    shallowEqual,
  );
  const quiet = useMetricsFrame((f): QuietView => {
    if (f.engine.state !== "running" || f.engine_failure) return NOT_QUIET;
    if (f.last_packet_at > 0) {
      return f.now - f.last_packet_at >= QUIET_AFTER_MS
        ? { since: f.last_packet_at, fromStart: false }
        : NOT_QUIET;
    }
    return f.uptime_s * 1000 >= QUIET_AFTER_MS
      ? {
          since: Math.floor((f.now - f.uptime_s * 1000) / 60_000) * 60_000,
          fromStart: true,
        }
      : NOT_QUIET;
  }, shallowEqual);
  const now = useServerNow(NOW_TICK_MS);
  const watchdogQuery = useWatchdogSetStatuses(watched.length > 0);
  const watchdog = useMemo(
    () => ({
      loaded: watchdogQuery.loaded,
      enabled: watchdogQuery.enabled,
      byId: watchdogQuery.byId,
    }),
    [watchdogQuery.loaded, watchdogQuery.enabled, watchdogQuery.byId],
  );
  const system = useSystemView();
  const bridge = useBridgeStatus();
  const { dismissed, dismiss } = useDismissals();

  const items = useMemo(
    () =>
      buildAttentionItems({
        t,
        locale,
        now,
        mode,
        quiet,
        watched,
        watchdog,
        attention,
        threads,
        errors,
        system,
        bridge,
        dismissed,
      }),
    [
      t,
      locale,
      now,
      mode,
      quiet,
      watched,
      watchdog,
      attention,
      threads,
      errors,
      system,
      bridge,
      dismissed,
    ],
  );

  if (items.length === 0) return null;

  return (
    <Box
      component="section"
      aria-labelledby={headingId}
      sx={{
        mb: 1.5,
        bgcolor: colors.background.paper,
        border: `1px solid ${colors.border.default}`,
        borderRadius: `${radiusPx.md}px`,
        overflow: "hidden",
      }}
    >
      <Box
        sx={{
          display: "flex",
          alignItems: "center",
          gap: "8px",
          px: "14px",
          pt: "10px",
          pb: "8px",
        }}
      >
        <Box
          component="h2"
          id={headingId}
          sx={{
            m: 0,
            fontSize: 12,
            fontWeight: 700,
            letterSpacing: "0.12em",
            textTransform: "uppercase",
            lineHeight: 1.35,
            color: colors.text.primary,
          }}
        >
          {t("dashboard.attention.title")}
        </Box>
        <Box
          component="span"
          sx={{
            fontSize: 12,
            fontWeight: 600,
            color: colors.text.secondary,
            fontVariantNumeric: "tabular-nums",
          }}
        >
          {t("dashboard.attention.count", { count: items.length })}
        </Box>
      </Box>
      <Box component="ul" id={listId} sx={{ listStyle: "none", m: 0, p: 0 }}>
        {visibleRows(items, expanded, COLLAPSED_ITEMS).map((item) => (
          <AttentionRow
            key={item.key}
            item={item}
            now={now}
            onDismiss={dismiss}
            onRestart={onRestart}
          />
        ))}
      </Box>
      <ShowMore
        total={items.length}
        expanded={expanded}
        limit={COLLAPSED_ITEMS}
        onToggle={() => setExpanded((value) => !value)}
        controls={listId}
      />
    </Box>
  );
});
