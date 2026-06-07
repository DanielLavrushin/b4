import { useEffect, useMemo, useState } from "react";
import { Box, Paper, Typography, Grid } from "@mui/material";
import { BlockOutlined as BlockIcon } from "@mui/icons-material";
import { colors, fonts, radiusPx } from "@design";
import { formatNumber } from "@utils";
import { B4CountPill } from "@b4.elements";
import { useTranslation } from "react-i18next";

interface DeviceInfo {
  mac: string;
  ip: string;
  hostname: string;
  vendor: string;
  alias?: string;
}

interface BlackholeProps {
  total: number;
  blockedDomains: Record<string, number>;
  blockedDevices: Record<string, number>;
}

export const Blackhole = ({
  total,
  blockedDomains,
  blockedDevices,
}: BlackholeProps) => {
  const { t } = useTranslation();
  const [devices, setDevices] = useState<DeviceInfo[]>([]);

  useEffect(() => {
    fetch("/api/devices")
      .then((r) => r.json())
      .then((data: { devices?: DeviceInfo[] }) => {
        if (data?.devices) setDevices(data.devices);
      })
      .catch(() => {});
  }, []);

  const deviceMap = useMemo(() => {
    const map: Record<string, DeviceInfo> = {};
    for (const d of devices) map[d.mac] = d;
    return map;
  }, [devices]);

  const getDeviceName = (mac: string): string => {
    const dev = deviceMap[mac];
    if (dev?.alias) return dev.alias;
    if (dev?.hostname) return dev.hostname;
    if (dev?.vendor && dev.vendor !== "Private") return `${dev.vendor} (${mac})`;
    return mac;
  };

  const topDomains = useMemo(
    () =>
      Object.entries(blockedDomains)
        .sort((a, b) => b[1] - a[1])
        .slice(0, 10),
    [blockedDomains],
  );

  const topDevices = useMemo(
    () =>
      Object.entries(blockedDevices)
        .sort((a, b) => b[1] - a[1])
        .slice(0, 10),
    [blockedDevices],
  );

  return (
    <Paper
      variant="outlined"
      sx={{
        bgcolor: colors.background.paper,
        borderColor: colors.border.default,
        borderRadius: `${radiusPx.md}px`,
        p: 0,
        overflow: "hidden",
      }}
    >
      <Box
        sx={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          gap: "12px",
          p: "12px 14px",
          borderBottom: `1px solid ${colors.border.light}`,
        }}
      >
        <Box sx={{ display: "flex", alignItems: "center", gap: "8px" }}>
          <BlockIcon sx={{ fontSize: 18, color: colors.state.error }} />
          <Typography
            variant="metricLabel"
            sx={{ color: colors.text.secondary, opacity: 0.85 }}
          >
            {t("dashboard.blackhole.title")}
          </Typography>
        </Box>
        <Box sx={{ display: "flex", alignItems: "baseline", gap: "8px" }}>
          <Box
            component="span"
            sx={{
              fontSize: 24,
              fontWeight: 700,
              color: colors.state.error,
              lineHeight: 1,
              fontFeatureSettings: '"tnum"',
            }}
          >
            {formatNumber(total)}
          </Box>
          <Box
            component="span"
            sx={{
              fontFamily: fonts.mono,
              fontSize: 11,
              color: colors.text.secondary,
            }}
          >
            {t("dashboard.blackhole.blocked")}
          </Box>
        </Box>
      </Box>

      <Grid container>
        <Grid
          size={{ xs: 12, sm: 6 }}
          sx={{ borderRight: { sm: `1px solid ${colors.border.light}` } }}
        >
          <BlockSection
            label={t("dashboard.blackhole.topDomains")}
            rows={topDomains.map(([domain, count]) => ({
              id: domain,
              name: domain,
              count,
            }))}
            empty={t("dashboard.blackhole.none")}
          />
        </Grid>
        <Grid size={{ xs: 12, sm: 6 }}>
          <BlockSection
            label={t("dashboard.blackhole.topDevices")}
            rows={topDevices.map(([mac, count]) => ({
              id: mac,
              name: getDeviceName(mac),
              count,
            }))}
            empty={t("dashboard.blackhole.none")}
          />
        </Grid>
      </Grid>
    </Paper>
  );
};

interface BlockRow {
  id: string;
  name: string;
  count: number;
}

interface BlockSectionProps {
  label: string;
  rows: BlockRow[];
  empty: string;
}

const BlockSection = ({ label, rows, empty }: BlockSectionProps) => (
  <Box>
    <Typography
      variant="metricLabel"
      sx={{
        display: "block",
        color: colors.text.secondary,
        opacity: 0.7,
        p: "10px 14px 4px",
      }}
    >
      {label}
    </Typography>
    {rows.length === 0 ? (
      <Box
        sx={{
          p: "8px 14px 12px",
          fontFamily: fonts.mono,
          fontSize: 11,
          color: colors.text.disabled,
        }}
      >
        {empty}
      </Box>
    ) : (
      rows.map(({ id, name, count }) => (
        <Box
          key={id}
          sx={{
            display: "flex",
            alignItems: "center",
            gap: "10px",
            p: "8px 14px",
            borderTop: `1px solid ${colors.border.light}`,
            "&:hover": { bgcolor: "rgba(255, 255, 255, 0.025)" },
          }}
        >
          <Box
            component="span"
            sx={{
              fontFamily: fonts.mono,
              fontSize: 11,
              letterSpacing: "0.04em",
              color: colors.text.primary,
              overflow: "hidden",
              textOverflow: "ellipsis",
              whiteSpace: "nowrap",
              flex: 1,
              minWidth: 0,
            }}
            title={name}
          >
            {name}
          </Box>
          <B4CountPill value={formatNumber(count)} />
        </Box>
      ))
    )}
  </Box>
);
