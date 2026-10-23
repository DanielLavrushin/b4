import { memo, useEffect, useId, useRef, useState } from "react";
import { Box, Link, Tooltip } from "@mui/material";
import { Link as RouterLink } from "react-router";
import { useTranslation } from "react-i18next";
import { colors, fonts } from "@design";
import { DeviceIcon } from "@b4.icons";
import type { DeviceInfo } from "@b4.devices";
import { formatInteger } from "@common/charts";
import { useDeviceNames } from "@hooks/useDeviceNames";
import type { DeviceEntry } from "@models/metrics";
import { useMetricsFrame } from "@/stores/useMetrics";
import { Ago } from "./Ago";
import { deviceTrafficLink, macDeviceLabel, type DeviceLabel } from "./deviceLabel";
import { formatSince } from "./format";
import { useHeldOrder } from "./heldOrder";
import { PanelCard } from "./PanelCard";
import { ShowMore, visibleRows } from "./ShowMore";
import { emptySx, listSx, numeric, rowSx } from "./styles";

const EMPTY_ENTRIES: readonly DeviceEntry[] = [];
const DEVICE_ROWS = 10;
const NAME_REFRESH_DELAY_MS = 2_000;
const MAC_PATTERN = /^[0-9a-f]{2}(:[0-9a-f]{2}){5}$/i;

const isMac = (key: string): boolean => MAC_PATTERN.test(key);

const isLoopback = (ip: string): boolean => ip === "::1" || ip.startsWith("127.");

const ellipsisSx = {
  display: "block",
  minWidth: 0,
  overflow: "hidden",
  textOverflow: "ellipsis",
  whiteSpace: "nowrap",
} as const;

interface DeviceRowProps {
  entry: DeviceEntry;
  device?: DeviceInfo;
  routerName?: string;
}

function labelFor({ entry, device, routerName }: DeviceRowProps): DeviceLabel {
  if (isMac(entry.key)) return macDeviceLabel(entry.key, device, entry.ip);
  if (routerName) return { name: routerName, meta: entry.key };
  return { name: entry.key };
}

const DeviceRow = memo(function DeviceRow(props: DeviceRowProps) {
  const { entry } = props;
  const { i18n } = useTranslation();
  const { name, meta, tip } = labelFor(props);
  const domains = entry.domains.join(", ");
  let title: string | undefined;
  if (!tip) title = meta ? `${name} (${meta})` : name;
  const who = (
    <Box sx={{ flex: "0 1 280px", minWidth: 0 }} title={title}>
      <Link
        component={RouterLink}
        underline="hover"
        to={deviceTrafficLink(entry.key, entry.ip)}
        sx={{
          ...ellipsisSx,
          fontSize: 13,
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
        {name}
      </Link>
      {meta && (
        <Box
          component="span"
          sx={{ ...ellipsisSx, fontFamily: fonts.mono, fontSize: 11, color: colors.text.secondary }}
        >
          {meta}
        </Box>
      )}
    </Box>
  );
  return (
    <Box component="li" sx={{ ...rowSx, display: "flex", alignItems: "center", gap: "12px" }}>
      <Box
        sx={{
          flex: "1 1 auto",
          minWidth: 0,
          display: "flex",
          flexWrap: "wrap",
          alignItems: "center",
          columnGap: "16px",
          rowGap: "2px",
        }}
      >
        {tip ? (
          <Tooltip title={tip} placement="top-start" describeChild>
            {who}
          </Tooltip>
        ) : (
          who
        )}
        {domains && (
          <Box
            component="span"
            title={domains}
            sx={{
              ...ellipsisSx,
              flex: "1 1 180px",
              fontFamily: fonts.mono,
              fontSize: 12,
              color: colors.text.secondary,
            }}
          >
            {domains}
          </Box>
        )}
      </Box>
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

function useNamesForNewDevices(
  entries: readonly DeviceEntry[],
  findDevice: (mac: string) => DeviceInfo | undefined,
  refresh: () => Promise<void>,
) {
  const asked = useRef(new Set<string>());
  const unknown = entries
    .filter((entry) => isMac(entry.key) && !findDevice(entry.key))
    .map((entry) => entry.key)
    .sort()
    .join(" ");
  useEffect(() => {
    const fresh = unknown.split(" ").filter((mac) => mac && !asked.current.has(mac));
    if (fresh.length === 0) return;
    const id = setTimeout(() => {
      for (const mac of fresh) asked.current.add(mac);
      void refresh();
    }, NAME_REFRESH_DELAY_MS);
    return () => clearTimeout(id);
  }, [unknown, refresh]);
}

function DevicesPanelView() {
  const { t, i18n } = useTranslation();
  const hasFrame = useMetricsFrame(() => true) ?? false;
  const entries = useMetricsFrame((f) => f.devices?.items) ?? EMPTY_ENTRIES;
  const statsSince = useMetricsFrame((f) => f.stats_since) ?? 0;
  const now = useMetricsFrame((f) => Math.floor(f.now / 60_000) * 60_000) ?? 0;
  const since = formatSince(statsSince, now, i18n.language);
  const { findDevice, routerIps, refresh } = useDeviceNames();
  const listId = useId();
  const [expanded, setExpanded] = useState(false);
  const { rows, holdProps } = useHeldOrder(entries);
  useNamesForNewDevices(entries, findDevice, refresh);
  const routerName = t("connections.aggregated.router");
  const isRouter = (key: string): boolean => routerIps.includes(key) || isLoopback(key);

  return (
    <PanelCard
      title={t("dashboard.devices.title")}
      subtitle={t("dashboard.devices.subtitle", { time: since })}
      icon={<DeviceIcon />}
      waiting={!hasFrame}
    >
      {rows.length === 0 ? (
        <Box sx={emptySx}>{t("dashboard.devices.empty", { time: since })}</Box>
      ) : (
        <>
          <Box component="ul" id={listId} sx={listSx} {...holdProps}>
            {visibleRows(rows, expanded, DEVICE_ROWS).map((entry) =>
              isMac(entry.key) ? (
                <DeviceRow key={entry.key} entry={entry} device={findDevice(entry.key)} />
              ) : (
                <DeviceRow
                  key={entry.key}
                  entry={entry}
                  routerName={isRouter(entry.key) ? routerName : undefined}
                />
              ),
            )}
          </Box>
          <ShowMore
            total={rows.length}
            expanded={expanded}
            onToggle={() => setExpanded((value) => !value)}
            limit={DEVICE_ROWS}
            controls={listId}
          />
        </>
      )}
    </PanelCard>
  );
}

export const DevicesPanel = memo(DevicesPanelView);
