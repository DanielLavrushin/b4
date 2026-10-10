import { memo, useMemo, useState } from "react";
import {
  Box,
  IconButton,
  List,
  ListItemButton,
  ListItemIcon,
  ListItemText,
  Menu,
  MenuItem,
  Stack,
  Tooltip,
  Typography,
  Divider,
} from "@mui/material";
import { CheckIcon, DeviceIcon, MenuIcon, SortIcon } from "@b4.icons";
import { colors } from "@design";
import { Sparkline } from "./Sparkline";
import { formatRelativeShort } from "@utils";
import type { EnrichedDevice } from "@hooks/useConnectionGroups";
import { useTranslation } from "react-i18next";

type DeviceSort = "name" | "recent" | "packets";

const DEVICE_SORTS: readonly DeviceSort[] = ["name", "recent", "packets"];
const DEVICE_SORT_STORAGE_KEY = "b4_connections_device_sort";
const DEVICE_SORT_LABELS: Record<DeviceSort, string> = {
  name: "connections.aggregated.sortByName",
  recent: "connections.aggregated.sortByRecent",
  packets: "connections.aggregated.sortByPackets",
};
const MAC_PATTERN = /^[0-9A-F]{2}(:[0-9A-F]{2}){5}$/i;
const labelCollator = new Intl.Collator(undefined, { numeric: true, sensitivity: "base" });

const loadDeviceSort = (): DeviceSort => {
  const stored = localStorage.getItem(DEVICE_SORT_STORAGE_KEY);
  return DEVICE_SORTS.find((s) => s === stored) ?? "name";
};

const nameTier = (d: EnrichedDevice): number => {
  if (d.deviceName) return 0;
  return MAC_PATTERN.test(d.mac) ? 1 : 2;
};

const primaryOrder = (sort: DeviceSort, a: EnrichedDevice, b: EnrichedDevice): number => {
  if (sort === "recent") return b.lastSeen - a.lastSeen;
  if (sort === "packets") return b.packets - a.packets;
  return 0;
};

const orderDevices = (devices: EnrichedDevice[], sort: DeviceSort): EnrichedDevice[] => {
  const keyed = devices.map((d) => ({ d, tier: nameTier(d), label: d.deviceName || d.mac }));
  keyed.sort(
    (a, b) =>
      primaryOrder(sort, a.d, b.d) ||
      a.tier - b.tier ||
      labelCollator.compare(a.label, b.label),
  );
  return keyed.map((k) => k.d);
};

interface Props {
  devices: EnrichedDevice[];
  selectedMac: string | null;
  onSelect: (mac: string | null) => void;
  collapsed: boolean;
  onToggleCollapsed: () => void;
  width?: number;
}

export const DeviceSidebar = memo<Props>(
  ({ devices, selectedMac, onSelect, collapsed, onToggleCollapsed, width = 240 }) => {
  const { t } = useTranslation();
  const [sort, setSort] = useState<DeviceSort>(loadDeviceSort);
  const [sortAnchor, setSortAnchor] = useState<HTMLElement | null>(null);
  const sorted = useMemo(() => orderDevices(devices, sort), [devices, sort]);
  const now = Date.now();

  const chooseSort = (next: DeviceSort) => {
    setSort(next);
    setSortAnchor(null);
    localStorage.setItem(DEVICE_SORT_STORAGE_KEY, next);
  };
  const totalPackets = devices.reduce((s, d) => s + d.packets, 0);

  if (collapsed) {
    return (
      <Box
        sx={{
          width: 36,
          flexShrink: 0,
          borderRight: `1px solid ${colors.border.light}`,
          bgcolor: colors.background.paper,
          display: "flex",
          flexDirection: "column",
          alignItems: "center",
          py: 1,
        }}
      >
        <Tooltip title={t("connections.aggregated.showDevices")} placement="right" arrow>
          <IconButton size="small" onClick={onToggleCollapsed} sx={{ color: colors.text.secondary }}>
            <MenuIcon sx={{ fontSize: 18 }} />
          </IconButton>
        </Tooltip>
      </Box>
    );
  }

  return (
    <Box
      sx={{
        width,
        flexShrink: 0,
        borderRight: `1px solid ${colors.border.light}`,
        bgcolor: colors.background.paper,
        overflow: "auto",
        display: "flex",
        flexDirection: "column",
      }}
    >
      <Stack
        direction="row"
        alignItems="center"
        sx={{
          px: 2,
          height: 32,
          borderBottom: `2px solid ${colors.border.default}`,
          bgcolor: colors.background.paper,
        }}
      >
        <Typography sx={{ color: colors.secondary, fontWeight: 600, fontSize: 14, flex: 1 }}>
          {t("connections.aggregated.devices")}
        </Typography>
        <Tooltip title={t("connections.aggregated.sortDevices")} placement="top" arrow>
          <IconButton
            size="small"
            aria-label={t("connections.aggregated.sortDevices")}
            aria-haspopup="menu"
            aria-expanded={sortAnchor ? "true" : undefined}
            onClick={(e) => setSortAnchor(e.currentTarget)}
            sx={{ color: colors.text.secondary }}
          >
            <SortIcon sx={{ fontSize: 16 }} />
          </IconButton>
        </Tooltip>
        <Menu
          anchorEl={sortAnchor}
          open={sortAnchor !== null}
          onClose={() => setSortAnchor(null)}
          anchorOrigin={{ vertical: "bottom", horizontal: "right" }}
          transformOrigin={{ vertical: "top", horizontal: "right" }}
        >
          {DEVICE_SORTS.map((s) => (
            <MenuItem
              key={s}
              role="menuitemradio"
              aria-checked={s === sort}
              onClick={() => chooseSort(s)}
            >
              <ListItemIcon>{s === sort && <CheckIcon fontSize="small" />}</ListItemIcon>
              <ListItemText>{t(DEVICE_SORT_LABELS[s])}</ListItemText>
            </MenuItem>
          ))}
        </Menu>
        <Tooltip title={t("connections.aggregated.hideDevices")} placement="right" arrow>
          <IconButton size="small" onClick={onToggleCollapsed} sx={{ color: colors.text.secondary }}>
            <MenuIcon sx={{ fontSize: 16 }} />
          </IconButton>
        </Tooltip>
      </Stack>
      <List dense disablePadding>
        <ListItemButton
          selected={selectedMac === null}
          onClick={() => onSelect(null)}
          sx={{
            py: 0.5,
            "&.Mui-selected": { bgcolor: colors.accent.primary },
            "&.Mui-selected:hover": { bgcolor: colors.accent.primaryHover },
          }}
        >
          <Stack direction="row" spacing={1} alignItems="center" sx={{ width: "100%" }}>
            <DeviceIcon sx={{ fontSize: 16, color: colors.text.disabled }} />
            <Typography sx={{ flex: 1, fontSize: 13 }}>
              {t("connections.aggregated.allDevices")}
            </Typography>
            <Typography sx={{ color: colors.text.disabled, fontSize: 11, fontFamily: "monospace" }}>
              {totalPackets}
            </Typography>
          </Stack>
        </ListItemButton>
        <Divider sx={{ borderColor: colors.border.light }} />
        {sorted.map((d) => {
          const label = d.deviceName || d.mac || t("connections.aggregated.unknownDevice");
          const isSelected = selectedMac === d.mac;
          return (
            <ListItemButton
              key={d.mac || "unknown"}
              selected={isSelected}
              onClick={() => onSelect(d.mac)}
              sx={{
                py: 0.5,
                "&.Mui-selected": { bgcolor: colors.accent.primary },
                "&.Mui-selected:hover": { bgcolor: colors.accent.primaryHover },
              }}
            >
              <Stack sx={{ width: "100%" }} spacing={0.2}>
                <Stack direction="row" spacing={1} alignItems="center">
                  <Typography
                    sx={{
                      flex: 1,
                      fontSize: 13,
                      color: colors.text.primary,
                      overflow: "hidden",
                      textOverflow: "ellipsis",
                      whiteSpace: "nowrap",
                    }}
                  >
                    {label}
                  </Typography>
                  <Typography
                    sx={{ color: colors.text.disabled, fontSize: 11, fontFamily: "monospace" }}
                  >
                    {formatRelativeShort(t, d.lastSeen, now)}
                  </Typography>
                </Stack>
                <Stack direction="row" spacing={1} alignItems="center">
                  <Box sx={{ flex: 1, minWidth: 0 }}>
                    <Sparkline data={d.buckets} width={100} height={16} />
                  </Box>
                  <Typography sx={{ color: colors.text.disabled, fontSize: 10, fontFamily: "monospace" }}>
                    {d.packets}
                  </Typography>
                </Stack>
              </Stack>
            </ListItemButton>
          );
        })}
      </List>
    </Box>
  );
});

DeviceSidebar.displayName = "DeviceSidebar";
