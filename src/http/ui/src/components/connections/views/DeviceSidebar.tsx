import { memo } from "react";
import { Box, IconButton, List, ListItemButton, Stack, Tooltip, Typography, Divider } from "@mui/material";
import { DeviceIcon, MenuIcon } from "@b4.icons";
import { colors } from "@design";
import { Sparkline } from "./Sparkline";
import { formatRelativeShort } from "@utils";
import type { EnrichedDevice } from "@hooks/useConnectionGroups";
import { useTranslation } from "react-i18next";

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
  const sorted = [...devices].sort((a, b) => b.lastSeen - a.lastSeen);
  const now = Date.now();
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
        sx={{
          alignItems: "center",
          px: 2,
          height: 32,
          borderBottom: `2px solid ${colors.border.default}`,
          bgcolor: colors.background.paper
        }}>
        <Typography sx={{ color: colors.secondary, fontWeight: 600, fontSize: 14, flex: 1 }}>
          {t("connections.aggregated.devices")}
        </Typography>
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
          <Stack
            direction="row"
            spacing={1}
            sx={{
              alignItems: "center",
              width: "100%"
            }}>
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
                <Stack direction="row" spacing={1} sx={{
                  alignItems: "center"
                }}>
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
                <Stack direction="row" spacing={1} sx={{
                  alignItems: "center"
                }}>
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
