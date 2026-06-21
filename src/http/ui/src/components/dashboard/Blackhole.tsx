import { useMemo } from "react";
import { Box, Typography, Grid } from "@mui/material";
import { BlockOutlined as BlockIcon } from "@mui/icons-material";
import { colors, fonts } from "@design";
import { formatNumber } from "@utils";
import { B4CountPill } from "@b4.elements";
import { useTranslation } from "react-i18next";
import { useDeviceNames } from "@hooks/useDeviceNames";
import { DashboardPanel } from "./DashboardPanel";
import { DataRow } from "./DataRow";
import { DomainLabel } from "./DomainLabel";

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
  const { getDeviceName } = useDeviceNames();

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
    <DashboardPanel
      icon={<BlockIcon sx={{ fontSize: 18, color: colors.state.error }} />}
      eyebrow={t("dashboard.blackhole.title")}
      divider
      right={
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
      }
    >
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
    </DashboardPanel>
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
        p: "12px 14px 4px",
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
        <DataRow key={id} right={<B4CountPill value={formatNumber(count)} />}>
          <DomainLabel value={name} uppercase={false} />
        </DataRow>
      ))
    )}
  </Box>
);
