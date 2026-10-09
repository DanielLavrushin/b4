import { memo, useMemo, type ReactNode } from "react";
import { Box, Link, Tooltip } from "@mui/material";
import { Link as RouterLink } from "react-router";
import { Trans, useTranslation } from "react-i18next";
import { colors, fonts, radiusPx } from "@design";
import { ReceivedIcon, SentIcon, TelegramIcon, WarningIcon } from "@b4.icons";
import { formatByteSize, formatInteger } from "@common/charts";
import { useTelegramBridgeSummary } from "@hooks/useTelegramBridge";
import type { MTProtoSecretStat, MTProtoStats } from "@models/metrics";
import type { TelegramBridgeStatus } from "@models/mtproto";
import { describeApiError } from "@utils";
import { useMetricsFrame } from "@/stores/useMetrics";
import { useAgoText } from "./Ago";
import { PanelCard } from "./PanelCard";
import { parseTimestamp } from "./format";
import { PANEL_PAD_X, emptySx, linkSx, numeric } from "./styles";

export function bridgeWorking(status: TelegramBridgeStatus): boolean {
  return (
    status.rule_installed &&
    status.listener.running &&
    !status.rule_shadowed_by &&
    !(status.tproxy.checked && !status.tproxy.available)
  );
}

const NARROW = "@container (max-width: 519.98px)";

const valueColor = (n: number): string => (n > 0 ? colors.text.primary : colors.text.disabled);

const strongSx = { fontWeight: 700, color: colors.text.primary } as const;

const tableSx = {
  width: "100%",
  borderCollapse: "collapse",
  "& th, & td": {
    ...numeric,
    px: "8px",
    py: "7px",
    fontSize: 13,
    lineHeight: 1.4,
    whiteSpace: "nowrap",
    textAlign: "right",
  },
  "& th:first-of-type": { pl: PANEL_PAD_X, textAlign: "left" },
  "& thead th": {
    py: "6px",
    fontSize: 12,
    fontWeight: 600,
    color: colors.text.secondary,
  },
  "& thead th, & thead td": { borderBottom: `1px solid ${colors.border.light}` },
  "& .filler": { width: "100%", p: 0 },
  "& tbody tr:nth-of-type(even)": { bgcolor: colors.background.default },
  [NARROW]: {
    "& thead": { display: "none" },
    "& tbody tr": {
      display: "flex",
      flexWrap: "wrap",
      alignItems: "baseline",
      columnGap: "14px",
      rowGap: "2px",
      px: PANEL_PAD_X,
      py: "8px",
    },
    "& tbody th, & tbody td": { display: "block", p: 0, textAlign: "left" },
    "& tbody th:first-of-type": { flexBasis: "100%", pl: 0 },
    "& td[data-label]::before": {
      content: "attr(data-label)",
      mr: "4px",
      fontWeight: 400,
      color: colors.text.secondary,
    },
    "& .filler": { display: "none" },
  },
} as const;

function SummaryStat({ icon, children }: { icon?: ReactNode; children: ReactNode }) {
  return (
    <Box
      component="span"
      sx={{
        ...numeric,
        display: "inline-flex",
        alignItems: "center",
        gap: "4px",
        whiteSpace: "nowrap",
        color: colors.text.secondary,
      }}
    >
      {icon && (
        <Box component="span" aria-hidden sx={{ display: "inline-flex", "& svg": { fontSize: 14 } }}>
          {icon}
        </Box>
      )}
      <span>{children}</span>
    </Box>
  );
}

function NetworksCell({ stat }: { stat: MTProtoSecretStat }) {
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
          textDecoration: "underline dotted",
          textUnderlineOffset: "3px",
          cursor: "help",
          color: valueColor(stat.networks),
        }}
      >
        {formatInteger(stat.networks, i18n.language)}
      </Box>
    </Tooltip>
  );
}

function SecretsTable({ secrets, ends }: { secrets: readonly MTProtoSecretStat[]; ends: boolean }) {
  const { t, i18n } = useTranslation();
  const locale = i18n.language;
  const labels = {
    networks: t("dashboard.telegram.colNetworks"),
    connections: t("dashboard.telegram.colConnections"),
    sent: t("dashboard.telegram.colSent"),
    received: t("dashboard.telegram.colReceived"),
  };
  return (
    <Box
      sx={{
        overflowX: "auto",
        borderTop: `1px solid ${colors.border.light}`,
        borderRadius: ends ? `0 0 ${radiusPx.md - 1}px ${radiusPx.md - 1}px` : 0,
      }}
    >
      <Box component="table" aria-label={t("dashboard.telegram.perSecret")} sx={tableSx}>
        <thead>
          <tr>
            <th scope="col">{t("dashboard.telegram.colSecret")}</th>
            <th scope="col">{labels.networks}</th>
            <th scope="col">{labels.connections}</th>
            <th scope="col">{labels.sent}</th>
            <th scope="col">{labels.received}</th>
            <td className="filler" />
          </tr>
        </thead>
        <tbody>
          {secrets.map((secret, index) => (
            <tr key={`${secret.name}-${index}`}>
              <Box
                component="th"
                scope="row"
                sx={{ fontWeight: 600, color: secret.name ? colors.text.primary : colors.text.disabled }}
              >
                <Box
                  component="span"
                  title={secret.name || undefined}
                  sx={{
                    display: "block",
                    maxWidth: 240,
                    overflow: "hidden",
                    textOverflow: "ellipsis",
                  }}
                >
                  {secret.name || t("dashboard.telegram.unnamed")}
                </Box>
              </Box>
              <td data-label={labels.networks}>
                <NetworksCell stat={secret} />
              </td>
              <Box component="td" data-label={labels.connections} sx={{ color: valueColor(secret.active) }}>
                {formatInteger(secret.active, locale)}
              </Box>
              <Box
                component="td"
                data-label={labels.sent}
                sx={{ fontWeight: 600, color: valueColor(secret.bytes_up) }}
              >
                {formatByteSize(secret.bytes_up, locale)}
              </Box>
              <Box
                component="td"
                data-label={labels.received}
                sx={{ fontWeight: 600, color: valueColor(secret.bytes_down) }}
              >
                {formatByteSize(secret.bytes_down, locale)}
              </Box>
              <td className="filler" />
            </tr>
          ))}
        </tbody>
      </Box>
    </Box>
  );
}

function MTProtoSection({ stats, ends }: { stats: MTProtoStats; ends: boolean }) {
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
            columnGap: "18px",
            rowGap: "4px",
            fontSize: 13,
          }}
        >
          <SummaryStat>
            <Trans
              i18nKey="dashboard.telegram.networksNow"
              count={stats.networks}
              values={{ value: formatInteger(stats.networks, locale) }}
              components={{ b: <Box component="strong" sx={strongSx} /> }}
            />
          </SummaryStat>
          <SummaryStat>
            <Trans
              i18nKey="dashboard.telegram.openConnections"
              count={stats.active_connections}
              values={{ value: formatInteger(stats.active_connections, locale) }}
              components={{ b: <Box component="strong" sx={strongSx} /> }}
            />
          </SummaryStat>
          <SummaryStat icon={<SentIcon />}>
            <Trans
              i18nKey="dashboard.telegram.sent"
              values={{ value: formatByteSize(stats.bytes_up, locale) }}
              components={{ b: <Box component="strong" sx={strongSx} /> }}
            />
          </SummaryStat>
          <SummaryStat icon={<ReceivedIcon />}>
            <Trans
              i18nKey="dashboard.telegram.received"
              values={{ value: formatByteSize(stats.bytes_down, locale) }}
              components={{ b: <Box component="strong" sx={strongSx} /> }}
            />
          </SummaryStat>
        </Box>
      </Box>
      {secrets.length === 0 ? (
        <Box sx={{ ...emptySx, borderTop: `1px solid ${colors.border.light}` }}>
          {t("dashboard.telegram.noSecrets")}
        </Box>
      ) : (
        <SecretsTable secrets={secrets} ends={ends} />
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
  const bridgeShown = bridgeOn || bridge.isError;

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
      {proxyOn && mtproto && <MTProtoSection stats={mtproto} ends={!bridgeShown} />}
      {bridgeShown && (
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
