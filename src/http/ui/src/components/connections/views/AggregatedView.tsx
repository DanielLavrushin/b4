import { useEffect, useMemo, useState } from "react";
import { Box, Fab, Tooltip } from "@mui/material";
import { StartIcon, StopIcon } from "@b4.icons";
import { colors } from "@design";
import { useConnectionGroups, type EnrichedGroup } from "@hooks/useConnectionGroups";
import { matchesConnectionFilter, parseConnectionFilter } from "@utils";
import { AggregatedControlBar, TimeWindow } from "./AggregatedControlBar";
import { DeviceSidebar } from "./DeviceSidebar";
import { GroupList } from "./GroupList";
import { DetailPane } from "./DetailPane";
import { useTranslation } from "react-i18next";

interface Props {
  lines: string[];
  deviceMap: Record<string, string>;
  paused: boolean;
  onTogglePause: () => void;
  showAll: boolean;
  onShowAllChange: (v: boolean) => void;
  onReset: () => void;
  filter: string;
  onFilterChange: (v: string) => void;
  enrichingIps: Set<string>;
  onAddDomain: (domain: string) => void;
  onAddIp: (ip: string) => void;
  onEnrichAsn: (ip: string) => void;
  onDeleteAsn: (asnId: string) => void;
}

const getGroupFieldValue = (g: EnrichedGroup, field: string): string => {
  switch (field) {
    case "asn":
      return g.asnName?.toLowerCase() || "";
    case "alias":
    case "device":
      return `${g.deviceName || ""} ${g.mac || ""}`.toLowerCase();
    case "domain":
      return g.domain.toLowerCase();
    case "destination":
      return g.destIp.toLowerCase();
    case "protocol":
      return g.protocol.toLowerCase();
    case "tls":
      return g.tls.toLowerCase();
    case "flags":
      return g.flags.toLowerCase();
    case "set":
      return `${g.hostSet || ""} ${g.ipSet || ""}`.toLowerCase();
    default:
      return "";
  }
};

const getGroupSearchableValues = (g: EnrichedGroup): (string | null)[] => [
  g.domain,
  g.destIp,
  g.asnName,
  g.hostSet,
  g.ipSet,
  g.deviceName,
  g.mac,
  g.protocol,
  g.tls,
  g.flags,
];

export const AggregatedView = ({
  lines,
  deviceMap,
  paused,
  onTogglePause,
  showAll,
  onShowAllChange,
  onReset,
  filter,
  onFilterChange,
  enrichingIps,
  onAddDomain,
  onAddIp,
  onEnrichAsn,
  onDeleteAsn,
}: Props) => {
  const { t } = useTranslation();
  const [window, setWindow] = useState<TimeWindow>(60);
  const [unmatchedOnly, setUnmatchedOnly] = useState(false);
  const [selectedMac, setSelectedMac] = useState<string | null>(null);
  const [selectedKey, setSelectedKey] = useState<string | null>(null);
  const [nowTick, setNowTick] = useState(() => Date.now());
  const [sidebarCollapsed, setSidebarCollapsed] = useState<boolean>(() => {
    return localStorage.getItem("b4_connections_sidebar_collapsed") === "1";
  });

  useEffect(() => {
    localStorage.setItem("b4_connections_sidebar_collapsed", sidebarCollapsed ? "1" : "0");
  }, [sidebarCollapsed]);

  const state = useConnectionGroups(lines, deviceMap, paused);

  useEffect(() => {
    const id = globalThis.setInterval(() => setNowTick(Date.now()), 1000);
    return () => globalThis.clearInterval(id);
  }, []);

  const filteredGroups = useMemo(() => {
    const cutoff = window === 0 ? 0 : nowTick - window * 1000;
    const parsedFilter = parseConnectionFilter(filter);
    return state.groups.filter((g) => {
      if (cutoff > 0 && g.lastSeen < cutoff) return false;
      if (unmatchedOnly && (g.hostSet || g.ipSet)) return false;
      if (!showAll && !g.domain) return false;
      if (selectedMac !== null && g.mac !== selectedMac) return false;
      if (
        parsedFilter &&
        !matchesConnectionFilter(
          parsedFilter,
          (field) => getGroupFieldValue(g, field),
          getGroupSearchableValues(g),
        )
      )
        return false;
      return true;
    });
  }, [state.groups, window, unmatchedOnly, showAll, selectedMac, filter, nowTick]);

  const sortedGroups = useMemo(
    () => [...filteredGroups].sort((a, b) => b.lastSeen - a.lastSeen || b.packets - a.packets),
    [filteredGroups],
  );

  const selectedGroup = useMemo(
    () => (selectedKey ? state.groups.find((g) => g.key === selectedKey) ?? null : null),
    [selectedKey, state.groups],
  );

  const visibleDevices = useMemo(() => {
    const cutoff = window === 0 ? 0 : nowTick - window * 1000;
    return state.devices.filter((d) => cutoff === 0 || d.lastSeen >= cutoff);
  }, [state.devices, window, nowTick]);

  return (
    <Box sx={{ flex: 1, display: "flex", flexDirection: "column", overflow: "hidden" }}>
      <AggregatedControlBar
        filter={filter}
        onFilterChange={onFilterChange}
        window={window}
        onWindowChange={setWindow}
        unmatchedOnly={unmatchedOnly}
        onUnmatchedOnlyChange={setUnmatchedOnly}
        showAll={showAll}
        onShowAllChange={onShowAllChange}
        onReset={onReset}
      />

      <Box sx={{ flex: 1, display: "flex", overflow: "hidden", position: "relative" }}>
        <DeviceSidebar
          devices={visibleDevices}
          selectedMac={selectedMac}
          onSelect={setSelectedMac}
          collapsed={sidebarCollapsed}
          onToggleCollapsed={() => setSidebarCollapsed((v) => !v)}
        />
        <GroupList
          groups={sortedGroups}
          now={nowTick}
          selectedKey={selectedKey}
          onSelect={(k) => setSelectedKey(k === selectedKey ? null : k)}
          onAddDomain={onAddDomain}
          onAddIp={onAddIp}
          onEnrichAsn={onEnrichAsn}
          enrichingIps={enrichingIps}
        />
        {selectedGroup && (
          <Box
            sx={{
              display: "flex",
              position: "absolute",
              top: 0,
              right: 0,
              bottom: 0,
              zIndex: 3,
              height: "100%",
              boxShadow: "-8px 0 24px rgba(0,0,0,0.5)",
            }}
          >
            <DetailPane
              group={selectedGroup}
              onClose={() => setSelectedKey(null)}
              onAddDomain={onAddDomain}
              onAddIp={onAddIp}
              onEnrichAsn={onEnrichAsn}
              onDeleteAsn={onDeleteAsn}
              enrichingIps={enrichingIps}
            />
          </Box>
        )}

        <Tooltip
          title={paused ? t("connections.page.resumeStreaming") : t("connections.page.pauseStreaming")}
          placement="left"
        >
          <Fab
            size="small"
            onClick={onTogglePause}
            sx={{
              position: "absolute",
              bottom: 16,
              right: 16,
              bgcolor: paused ? colors.secondary : colors.border.strong,
              color: colors.background.default,
              "&:hover": { bgcolor: paused ? colors.secondary : colors.border.default },
            }}
          >
            {paused ? <StartIcon /> : <StopIcon />}
          </Fab>
        </Tooltip>
      </Box>
    </Box>
  );
};
