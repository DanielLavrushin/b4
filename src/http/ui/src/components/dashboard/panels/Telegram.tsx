import { memo, useMemo } from "react";
import { Box, Link, Tooltip } from "@mui/material";
import { Link as RouterLink } from "react-router";
import { useTranslation } from "react-i18next";
import { colors, fonts } from "@design";
import { TelegramIcon, WarningIcon } from "@b4.icons";
import { formatByteSize, formatInteger } from "@common/charts";
import { useTelegramBridgeSummary } from "@hooks/useTelegramBridge";
import type { MTProtoSecretStat, MTProtoStats } from "@models/metrics";
import type { TelegramBridgeStatus } from "@models/mtproto";
import { describeApiError } from "@utils";
import { useMetricsFrame } from "@/stores/useMetrics";
import { useAgoText } from "./Ago";
import { PanelCard } from "./PanelCard";
import { parseTimestamp } from "./format";
import {
  PANEL_PAD_X,
  emptySx,
  linkSx,
  listSx,
  numeric,
  rowSx,
} from "./styles";

export function bridgeWorking(status: TelegramBridgeStatus): boolean {
  return (
    status.rule_installed &&
    status.listener.running &&
    !status.rule_shadowed_by &&
    !(status.tproxy.checked && !status.tproxy.available)
  );
}

const statSx = {
  ...numeric,
  whiteSpace: "nowrap",
} as const;

function NetworksLabel({ stat }: { stat: MTProtoSecretStat }) {
  const { t, i18n } = useTranslation();
  const hidden = Math.max(0, stat.networks - stat.network_addrs.length);
  const tip = (
    <Box sx={{ fontSize: 12, lineHeight: 1.5 }}>
      <Box sx={{ mb: stat.network_addrs.length > 0 ? "4px" : 0 }}>
        {stat.networks > 0
          ? t("dashboard.telegram.networksHint")
          : t("dashboard.telegram.networksNone")}
      </Box>
      {stat.network_addrs.map((addr) => (
        <Box key={addr} sx={{ fontFamily: fonts.mono, fontSize: 11 }}>
          {addr}
        </Box>
      ))}
      {hidden > 0 && (
        <Box sx={{ fontFamily: fonts.mono, fontSize: 11 }}>
          {t("dashboard.telegram.networksMore", {
            value: formatInteger(hidden, i18n.language),
          })}
        </Box>
      )}
    </Box>
  );
  return (
    <Tooltip title={tip} describeChild>
      <Box
        component="span"
        sx={{
          ...statSx,
          textDecoration: "underline dotted",
          textUnderlineOffset: "3px",
          cursor: "help",
          color: stat.networks > 0 ? colors.text.primary : colors.text.secondary,
        }}
      >
        {t("dashboard.telegram.networksNow", {
          count: stat.networks,
          value: formatInteger(stat.networks, i18n.language),
        })}
      </Box>
    </Tooltip>
  );
}

function MTProtoSection({ stats }: { stats: MTProtoStats }) {
  const { t, i18n } = useTranslation();
  const locale = i18n.language;
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
    <Box>
      <Box sx={{ px: PANEL_PAD_X, pb: "10px" }}>
        <Box
          component="h3"
          sx={{ m: 0, mb: "4px", fontSize: 13, fontWeight: 600, color: colors.text.primary }}
        >
          {t("dashboard.telegram.proxyTitle", {
            port: stats.port > 0 ? String(stats.port) : "-",
          })}
        </Box>
        <Box
          sx={{
            display: "flex",
            flexWrap: "wrap",
            columnGap: "16px",
            rowGap: "2px",
            fontSize: 13,
            color: colors.text.primary,
          }}
        >
          <Box component="span" sx={statSx}>
            {t("dashboard.telegram.networksNow", {
              count: stats.networks,
              value: formatInteger(stats.networks, locale),
            })}
          </Box>
          <Box component="span" sx={statSx}>
            {t("dashboard.telegram.openConnections", {
              count: stats.active_connections,
              value: formatInteger(stats.active_connections, locale),
            })}
          </Box>
          <Box component="span" sx={{ ...statSx, color: colors.text.secondary }}>
            {t("dashboard.telegram.sent", { value: formatByteSize(stats.bytes_up, locale) })}
          </Box>
          <Box component="span" sx={{ ...statSx, color: colors.text.secondary }}>
            {t("dashboard.telegram.received", {
              value: formatByteSize(stats.bytes_down, locale),
            })}
          </Box>
        </Box>
      </Box>
      {secrets.length === 0 ? (
        <Box sx={{ ...emptySx, borderTop: `1px solid ${colors.border.light}` }}>
          {t("dashboard.telegram.noSecrets")}
        </Box>
      ) : (
        <Box component="ul" sx={listSx} aria-label={t("dashboard.telegram.perSecret")}>
          {secrets.map((secret, index) => (
            <Box
              component="li"
              key={`${secret.name}-${index}`}
              sx={{
                ...rowSx,
                display: "flex",
                flexWrap: "wrap",
                alignItems: "baseline",
                columnGap: "16px",
                rowGap: "2px",
              }}
            >
              <Box
                component="span"
                sx={{
                  flex: "1 1 120px",
                  minWidth: 0,
                  overflow: "hidden",
                  textOverflow: "ellipsis",
                  whiteSpace: "nowrap",
                  fontSize: 13,
                  fontWeight: 600,
                  color: secret.name ? colors.text.primary : colors.text.secondary,
                }}
              >
                {secret.name || t("dashboard.telegram.unnamed")}
              </Box>
              <Box
                sx={{
                  display: "flex",
                  flexWrap: "wrap",
                  columnGap: "14px",
                  rowGap: "2px",
                  fontSize: 12,
                  color: colors.text.secondary,
                }}
              >
                <NetworksLabel stat={secret} />
                <Box component="span" sx={statSx}>
                  {t("dashboard.telegram.openConnections", {
                    count: secret.active,
                    value: formatInteger(secret.active, locale),
                  })}
                </Box>
                <Box component="span" sx={statSx}>
                  {t("dashboard.telegram.sent", {
                    value: formatByteSize(secret.bytes_up, locale),
                  })}
                </Box>
                <Box component="span" sx={statSx}>
                  {t("dashboard.telegram.received", {
                    value: formatByteSize(secret.bytes_down, locale),
                  })}
                </Box>
              </Box>
            </Box>
          ))}
        </Box>
      )}
    </Box>
  );
}

function BridgeLine({ status }: { status: TelegramBridgeStatus }) {
  const { t, i18n } = useTranslation();
  const locale = i18n.language;
  const { stats } = status;
  const ago = useAgoText(parseTimestamp(stats.last_relayed_at));
  const parts: string[] = [];
  if (stats.relayed > 0) {
    parts.push(
      t("dashboard.telegram.bridgeRelayed", {
        count: stats.relayed,
        value: formatInteger(stats.relayed, locale),
      }),
    );
    if (ago) parts.push(t("dashboard.telegram.bridgeLast", { ago }));
  } else {
    parts.push(t("dashboard.telegram.bridgeNothing"));
  }
  if (stats.dial_failed > 0) {
    parts.push(
      t("dashboard.telegram.bridgeDialFailures", {
        count: stats.dial_failed,
        value: formatInteger(stats.dial_failed, locale),
      }),
    );
  }
  const working = bridgeWorking(status);
  return (
    <Box
      sx={{
        display: "flex",
        flexWrap: "wrap",
        alignItems: "center",
        columnGap: "12px",
        rowGap: "2px",
        fontSize: 13,
        lineHeight: 1.5,
        color: colors.text.primary,
      }}
    >
      <Box component="span" sx={numeric}>
        {t("dashboard.telegram.bridgeLine", {
          parts: parts.join(t("dashboard.telegram.separator")),
        })}
      </Box>
      {!working && (
        <Box
          component="span"
          sx={{ display: "inline-flex", alignItems: "center", gap: "4px" }}
        >
          <Box
            component="span"
            aria-hidden
            sx={{ display: "inline-flex", color: colors.state.warning, "& svg": { fontSize: 15 } }}
          >
            <WarningIcon />
          </Box>
          {t("dashboard.telegram.bridgeNotWorking")}
        </Box>
      )}
    </Box>
  );
}

function TelegramPanelView() {
  const { t } = useTranslation();
  const hasFrame = useMetricsFrame(() => true) ?? false;
  const mtproto = useMetricsFrame((f) => f.mtproto);
  const bridge = useTelegramBridgeSummary();
  const bridgeData = bridge.data;
  const bridgeOn = bridgeData?.enabled === true;
  const proxyOn = mtproto?.enabled === true;

  return (
    <PanelCard
      title={t("dashboard.telegram.title")}
      icon={<TelegramIcon />}
      waiting={!hasFrame}
      actions={
        <Link component={RouterLink} underline="hover" to="/settings/mtproto" sx={linkSx}>
          {t("dashboard.telegram.settings")}
        </Link>
      }
    >
      {proxyOn && mtproto && <MTProtoSection stats={mtproto} />}
      {(bridgeOn || bridge.isError) && (
        <Box
          sx={{
            px: PANEL_PAD_X,
            py: "10px",
            borderTop: proxyOn ? `1px solid ${colors.border.light}` : undefined,
          }}
        >
          {bridgeData && bridgeOn ? (
            <BridgeLine status={bridgeData} />
          ) : (
            <Box sx={{ fontSize: 12, color: colors.text.secondary }}>
              {t("dashboard.telegram.bridgeUnavailable", {
                error: describeApiError(bridge.error),
              })}
            </Box>
          )}
        </Box>
      )}
      {!proxyOn && !bridgeOn && !bridge.isError && (
        <Box sx={emptySx}>
          {bridge.isPending
            ? t("dashboard.telegram.loading")
            : t("dashboard.telegram.off")}
        </Box>
      )}
    </PanelCard>
  );
}

export const TelegramPanel = memo(TelegramPanelView);
