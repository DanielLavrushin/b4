import { useMemo } from "react";
import { Box, Tooltip, Typography } from "@mui/material";
import { colors, fonts } from "@design";
import { formatBytes, formatNumber } from "@utils";
import { TelegramIcon } from "@b4.icons";
import { useTranslation } from "react-i18next";
import { DashboardPanel } from "./DashboardPanel";
import { DataRow } from "./DataRow";
import { MTProtoSecretStat, MTProtoStats } from "./Page";

const NetworkList = ({ stat }: { stat: MTProtoSecretStat }) => {
  const { t } = useTranslation();
  const hidden = stat.networks - stat.network_addrs.length;
  return (
    <Box sx={{ fontFamily: fonts.mono, fontSize: 11 }}>
      <Box sx={{ mb: stat.network_addrs.length ? 0.5 : 0 }}>
        {stat.networks > 0
          ? t("dashboard.mtproto.networksTooltip")
          : t("dashboard.mtproto.networksNone")}
      </Box>
      {stat.network_addrs.map((a) => (
        <Box key={a}>{a}</Box>
      ))}
      {hidden > 0 ? <Box>{t("dashboard.mtproto.networksMore", { n: hidden })}</Box> : null}
    </Box>
  );
};

interface MTProtoActivityProps {
  stats: MTProtoStats;
}

export const MTProtoActivity = ({ stats }: MTProtoActivityProps) => {
  const { t } = useTranslation();

  const secrets = useMemo(
    () =>
      [...stats.secrets].sort(
        (a, b) =>
          b.active - a.active ||
          b.bytes_down + b.bytes_up - (a.bytes_down + a.bytes_up),
      ),
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
            {formatNumber(stats.networks)}
          </Box>
          <Box
            component="span"
            sx={{
              fontFamily: fonts.mono,
              fontSize: 11,
              color: colors.text.secondary,
            }}
          >
            {t("dashboard.mtproto.networksLabel", { count: stats.networks })},{" "}
            {formatNumber(stats.active_connections)}{" "}
            {t("dashboard.mtproto.connLabel")}
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
                  flexWrap: "wrap",
                  justifyContent: "flex-end",
                  fontFamily: fonts.mono,
                  fontSize: 11,
                  color: colors.text.secondary,
                  whiteSpace: "nowrap",
                }}
              >
                <span>↑ {formatBytes(s.bytes_up)}</span>
                <span>↓ {formatBytes(s.bytes_down)}</span>
                <span>
                  {t("dashboard.mtproto.now")}:{" "}
                  <Tooltip title={<NetworkList stat={s} />} placement="top-end">
                    <Box
                      component="span"
                      tabIndex={0}
                      sx={{
                        color:
                          s.networks > 0
                            ? colors.text.primary
                            : colors.text.disabled,
                        fontWeight: 700,
                        cursor: "help",
                        textDecoration: "underline dotted",
                        textUnderlineOffset: "3px",
                      }}
                    >
                      {formatNumber(s.networks)}{" "}
                      {t("dashboard.mtproto.networksLabel", {
                        count: s.networks,
                      })}
                    </Box>
                  </Tooltip>
                  {", "}
                  <Box
                    component="span"
                    sx={{
                      color:
                        s.active > 0 ? colors.state.info : colors.text.disabled,
                      fontWeight: 700,
                    }}
                  >
                    {formatNumber(s.active)}{" "}
                    {t("dashboard.mtproto.connLabel")}
                  </Box>
                </span>
              </Box>
            }
          >
            <Typography
              sx={{
                fontSize: 13,
                fontWeight: 600,
                color: colors.text.primary,
                flex: "1 1 auto",
                minWidth: 64,
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
