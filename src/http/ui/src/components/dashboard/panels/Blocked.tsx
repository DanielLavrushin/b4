import { memo, useId, useState, type ReactNode } from "react";
import { Box, Link, Tooltip } from "@mui/material";
import { Link as RouterLink } from "react-router";
import { useTranslation } from "react-i18next";
import { colors, fonts } from "@design";
import { BlockIcon } from "@b4.icons";
import { formatInteger } from "@common/charts";
import { useDeviceNames } from "@hooks/useDeviceNames";
import type { BlockedEntry } from "@models/metrics";
import { useMetricsFrame } from "@/stores/useMetrics";
import { Ago } from "./Ago";
import { PanelCard } from "./PanelCard";
import { ShowMore, visibleRows } from "./ShowMore";
import { formatSince } from "./format";
import { PANEL_PAD_X, emptySx, listSx, numeric, rowSx, sectionLabelSx } from "./styles";

const EMPTY_ENTRIES: readonly BlockedEntry[] = [];
const BLOCKED_ROWS = 5;

interface BlockedRowProps {
  entry: BlockedEntry;
  name: string;
  meta?: string;
  tip?: ReactNode;
  to?: string;
}

function DeviceTip({ mac, vendor }: { mac: string; vendor: string }) {
  const { t } = useTranslation();
  return (
    <Box sx={{ fontSize: 12, lineHeight: 1.5 }}>
      <Box sx={{ fontFamily: fonts.mono, fontSize: 11 }}>
        {t("dashboard.blocked.mac", { value: mac })}
      </Box>
      {vendor && <Box>{vendor}</Box>}
    </Box>
  );
}

const BlockedRow = memo(function BlockedRow({ entry, name, meta, tip, to }: BlockedRowProps) {
  const { i18n } = useTranslation();
  const label = (
    <Box
      component="span"
      sx={{
        display: "block",
        overflow: "hidden",
        textOverflow: "ellipsis",
        whiteSpace: "nowrap",
        fontFamily: to ? undefined : fonts.mono,
        fontSize: to ? 13 : 12,
      }}
    >
      {name}
    </Box>
  );
  let title: string | undefined;
  if (!tip) title = meta ? `${name} (${meta})` : name;
  const info = (
    <Box sx={{ flex: "1 1 auto", minWidth: 0 }} title={title}>
      {to ? (
        <Link
          component={RouterLink}
          underline="hover"
          to={to}
          sx={{
            display: "block",
            minWidth: 0,
            color: colors.text.primary,
            textUnderlineOffset: "3px",
            "&:hover": { color: colors.text.primary, textDecoration: "underline" },
            "&:focus-visible": {
              outline: `2px solid ${colors.border.strong}`,
              outlineOffset: "2px",
              borderRadius: "2px",
            },
          }}
        >
          {label}
        </Link>
      ) : (
        <Box sx={{ color: colors.text.primary }}>{label}</Box>
      )}
      {meta && (
        <Box
          component="span"
          sx={{
            display: "block",
            fontFamily: fonts.mono,
            fontSize: 11,
            color: colors.text.secondary,
            overflow: "hidden",
            textOverflow: "ellipsis",
            whiteSpace: "nowrap",
          }}
        >
          {meta}
        </Box>
      )}
    </Box>
  );
  return (
    <Box
      component="li"
      sx={{
        ...rowSx,
        display: "flex",
        alignItems: "center",
        gap: "12px",
      }}
    >
      {tip ? (
        <Tooltip title={tip} placement="top-start" describeChild>
          {info}
        </Tooltip>
      ) : (
        info
      )}
      <Box
        component="span"
        sx={{
          ...numeric,
          flexShrink: 0,
          fontSize: 13,
          fontWeight: 700,
          color: colors.text.primary,
        }}
      >
        {formatInteger(entry.count, i18n.language)}
      </Box>
      <Box
        component="span"
        sx={{
          flexShrink: 0,
          minWidth: 72,
          textAlign: "right",
          fontSize: 12,
          color: colors.text.secondary,
        }}
      >
        <Ago at={entry.last} />
      </Box>
    </Box>
  );
});

interface BlockedListProps {
  title: string;
  empty: string;
  entries: readonly BlockedEntry[];
  render: (entry: BlockedEntry) => Omit<BlockedRowProps, "entry">;
}

function BlockedList({ title, empty, entries, render }: BlockedListProps) {
  const listId = useId();
  const headingId = useId();
  const [expanded, setExpanded] = useState(false);
  return (
    <Box sx={{ flex: "1 1 0", minWidth: 0 }}>
      <Box
        component="h3"
        id={headingId}
        sx={{ ...sectionLabelSx, m: 0, px: PANEL_PAD_X, pt: "10px", pb: "6px" }}
      >
        {title}
      </Box>
      {entries.length === 0 ? (
        <Box sx={{ ...emptySx, pt: "4px" }}>{empty}</Box>
      ) : (
        <>
          <Box component="ul" id={listId} aria-labelledby={headingId} sx={listSx}>
            {visibleRows(entries, expanded, BLOCKED_ROWS).map((entry) => (
              <BlockedRow key={entry.key} entry={entry} {...render(entry)} />
            ))}
          </Box>
          <ShowMore
            total={entries.length}
            expanded={expanded}
            onToggle={() => setExpanded((value) => !value)}
            limit={BLOCKED_ROWS}
            controls={listId}
          />
        </>
      )}
    </Box>
  );
}

function BlockedPanelView() {
  const { t, i18n } = useTranslation();
  const locale = i18n.language;
  const { deviceMap, getDeviceName } = useDeviceNames();
  const hasFrame = useMetricsFrame(() => true) ?? false;
  const blockedDns = useMetricsFrame((f) => f.totals.blocked_dns) ?? 0;
  const blockedConns = useMetricsFrame((f) => f.totals.blocked_conns) ?? 0;
  const statsSince = useMetricsFrame((f) => f.stats_since) ?? 0;
  const now = useMetricsFrame((f) => Math.floor(f.now / 60_000) * 60_000) ?? 0;
  const domains = useMetricsFrame((f) => f.blocked?.domains) ?? EMPTY_ENTRIES;
  const devices = useMetricsFrame((f) => f.blocked?.devices) ?? EMPTY_ENTRIES;
  const since = formatSince(statsSince, now, locale);

  return (
    <PanelCard
      title={t("dashboard.blocked.title")}
      icon={<BlockIcon />}
      waiting={!hasFrame}
    >
      <Box sx={{ px: PANEL_PAD_X, pb: "10px" }}>
        <Box sx={{ ...numeric, fontSize: 13, lineHeight: 1.5, color: colors.text.primary }}>
          {t("dashboard.blocked.totals", {
            time: since,
            dns: t("dashboard.blocked.dnsLookups", {
              count: blockedDns,
              value: formatInteger(blockedDns, locale),
            }),
            conns: t("dashboard.blocked.connections", {
              count: blockedConns,
              value: formatInteger(blockedConns, locale),
            }),
          })}
        </Box>
        <Box sx={{ mt: "4px", fontSize: 12, lineHeight: 1.5, color: colors.text.secondary }}>
          {t("dashboard.blocked.note")}
        </Box>
      </Box>
      <Box
        sx={{
          display: "flex",
          flexDirection: "column",
          borderTop: `1px solid ${colors.border.light}`,
          "@container (min-width: 720px)": {
            flexDirection: "row",
            "& > :first-of-type": { borderRight: `1px solid ${colors.border.light}` },
          },
          "@container (max-width: 719.98px)": {
            "& > :first-of-type": { borderBottom: `1px solid ${colors.border.light}` },
          },
        }}
      >
        <BlockedList
          title={t("dashboard.blocked.domains")}
          empty={t("dashboard.blocked.noDomains", { time: since })}
          entries={domains}
          render={(entry) => ({ name: entry.key })}
        />
        <BlockedList
          title={t("dashboard.blocked.devices")}
          empty={t("dashboard.blocked.noDevices", { time: since })}
          entries={devices}
          render={(entry) => {
            const mac = entry.key;
            const device = deviceMap[mac];
            const name = getDeviceName(mac);
            const vendor =
              device?.vendor && device.vendor !== "Private" && !name.includes(device.vendor)
                ? device.vendor
                : "";
            const showsMac = name.includes(mac);
            const meta = device?.ip || (showsMac ? undefined : mac);
            return {
              name,
              meta,
              tip: showsMac || meta === mac ? undefined : <DeviceTip mac={mac} vendor={vendor} />,
              to: `/traffic?device=${encodeURIComponent(mac)}`,
            };
          }}
        />
      </Box>
    </PanelCard>
  );
}

export const BlockedPanel = memo(BlockedPanelView);
