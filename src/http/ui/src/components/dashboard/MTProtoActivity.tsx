import { useMemo } from "react";
import { Box, Typography } from "@mui/material";
import { colors, fonts } from "@design";
import { formatBytes, formatNumber } from "@utils";
import { TelegramIcon } from "@b4.icons";
import { useTranslation } from "react-i18next";
import { DashboardPanel } from "./DashboardPanel";
import { DataRow } from "./DataRow";
import { MTProtoStats } from "./Page";

interface MTProtoActivityProps {
  stats: MTProtoStats;
}

export const MTProtoActivity = ({ stats }: MTProtoActivityProps) => {
  const { t } = useTranslation();

  const secrets = useMemo(
    () => [...stats.secrets].sort((a, b) => b.active - a.active || b.total - a.total),
    [stats.secrets],
  );

  return (
    <DashboardPanel
      icon={<TelegramIcon sx={{ fontSize: 18, color: colors.state.info }} />}
      eyebrow={t("dashboard.mtproto.title")}
      divider
      right={
        <Box sx={{ display: "flex", alignItems: "baseline", gap: "8px" }}>
          <Box
            component="span"
            sx={{
              fontSize: 24,
              fontWeight: 700,
              color: colors.state.info,
              lineHeight: 1,
              fontFeatureSettings: '"tnum"',
            }}
          >
            {formatNumber(stats.active_connections)}
          </Box>
          <Box
            component="span"
            sx={{
              fontFamily: fonts.mono,
              fontSize: 11,
              color: colors.text.secondary,
            }}
          >
            {t("dashboard.mtproto.active")}
          </Box>
        </Box>
      }
    >
      <Box
        sx={{
          display: "flex",
          gap: "16px",
          flexWrap: "wrap",
          p: "10px 14px",
          fontFamily: fonts.mono,
          fontSize: 11,
          color: colors.text.secondary,
        }}
      >
        <span>
          {t("dashboard.mtproto.port")}: <b>{stats.port || "—"}</b>
        </span>
        <span>
          {t("dashboard.mtproto.sessions")}:{" "}
          <b>{formatNumber(stats.total_connections)}</b>
        </span>
        <span>↑ {formatBytes(stats.bytes_up)}</span>
        <span>↓ {formatBytes(stats.bytes_down)}</span>
      </Box>

      <Typography
        variant="metricLabel"
        sx={{
          display: "block",
          color: colors.text.secondary,
          opacity: 0.7,
          p: "8px 14px 4px",
        }}
      >
        {t("dashboard.mtproto.perUser")}
      </Typography>

      {secrets.length === 0 ? (
        <Box
          sx={{
            p: "8px 14px 12px",
            fontFamily: fonts.mono,
            fontSize: 11,
            color: colors.text.disabled,
          }}
        >
          {t("dashboard.mtproto.none")}
        </Box>
      ) : (
        secrets.map((s, i) => (
          <DataRow
            key={`${s.name}-${i}`}
            right={
              <Box
                sx={{
                  display: "flex",
                  gap: "12px",
                  alignItems: "baseline",
                  fontFamily: fonts.mono,
                  fontSize: 11,
                  color: colors.text.secondary,
                  whiteSpace: "nowrap",
                }}
              >
                <span>↑ {formatBytes(s.bytes_up)}</span>
                <span>↓ {formatBytes(s.bytes_down)}</span>
                <span>
                  {formatNumber(s.total)} {t("dashboard.mtproto.sessionsShort")}
                </span>
                <Box
                  component="span"
                  sx={{
                    color:
                      s.active > 0 ? colors.state.info : colors.text.disabled,
                    fontWeight: 700,
                  }}
                >
                  {formatNumber(s.active)} {t("dashboard.mtproto.activeShort")}
                </Box>
              </Box>
            }
          >
            <Typography
              sx={{
                fontSize: 13,
                fontWeight: 600,
                color: colors.text.primary,
                overflow: "hidden",
                textOverflow: "ellipsis",
                whiteSpace: "nowrap",
              }}
            >
              {s.name || t("dashboard.mtproto.unnamed")}
            </Typography>
          </DataRow>
        ))
      )}
    </DashboardPanel>
  );
};
