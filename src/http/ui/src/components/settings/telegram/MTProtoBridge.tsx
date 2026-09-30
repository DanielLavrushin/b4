import { Fragment } from "react";
import { Trans, useTranslation } from "react-i18next";
import { Link as RouterLink } from "react-router";
import {
  Box,
  Button,
  Chip,
  CircularProgress,
  Grid,
  Link,
  Stack,
  Tooltip,
  Typography,
} from "@mui/material";
import { CloudDownloadIcon, NetworkIcon, RefreshIcon } from "@b4.icons";
import { B4Alert, B4Hint, B4IntegrationCard } from "@b4.elements";
import { colors, fonts, radiusPx, typography } from "@design";
import { B4Config } from "@models/config";
import { TelegramBridgeStatus } from "@models/mtproto";
import { SettingsPropHandlerType } from "@models/settings";
import { useSnackbar } from "@context/SnackbarProvider";
import {
  useCheckTelegramBridge,
  useRefreshTelegramAddresses,
  useTelegramBridgeStatus,
} from "@hooks/useTelegramBridge";
import { describeApiError, formatNumber, formatTimeAgo } from "@utils";

interface MTProtoBridgeCardProps {
  config: B4Config;
  savedEnabled: boolean;
  onChange: (field: string, value: SettingsPropHandlerType) => void;
}

const K = (key: string) => `settings.TelegramBridge.${key}`;

interface StatTileProps {
  label: string;
  value: string;
  hint?: string;
}

const StatTile = ({ label, value, hint }: StatTileProps) => (
  <Grid size={{ xs: 6, md: 3 }}>
    <Typography
      component="div"
      sx={{
        ...typography.recipes.metricLabel,
        fontWeight: typography.weights.bold,
        color: colors.text.disabled,
        mb: 0.75,
      }}
    >
      {label}
    </Typography>
    <Typography
      component="div"
      sx={{
        fontFamily: fonts.mono,
        fontSize: typography.sizes.lg,
        color: colors.text.primary,
        lineHeight: 1.3,
      }}
    >
      {value}
    </Typography>
    {hint && (
      <Typography
        component="div"
        variant="caption"
        sx={{ color: colors.text.secondary, mt: 0.25 }}
      >
        {hint}
      </Typography>
    )}
  </Grid>
);

const BridgeStats = ({ data }: { data: TelegramBridgeStatus }) => {
  const { t } = useTranslation();
  const { listener, addresses, stats } = data;
  const families = [listener.v4 && "IPv4", listener.v6 && "IPv6"]
    .filter(Boolean)
    .join(" + ");
  const listenerHint = listener.running ? families : t(K("listenerStopped"));
  const source = t(K(`source.${addresses.source}`), {
    defaultValue: addresses.source,
  });
  const addressHint = addresses.updated_at
    ? `${source} · ${t(K("updatedAgo"), { ago: formatTimeAgo(t, addresses.updated_at) })}`
    : source;
  const relayedHint = stats.last_relayed_at
    ? t(K("lastRelayed"), { ago: formatTimeAgo(t, stats.last_relayed_at) })
    : t(K("neverRelayed"));
  const showCounters =
    stats.failed_open > 0 || stats.dial_failed > 0 || stats.dropped > 0;

  return (
    <Stack spacing={1.25}>
      <Grid container spacing={2}>
        <StatTile
          label={t(K("listenerPort"))}
          value={listener.port ? String(listener.port) : "-"}
          hint={listenerHint}
        />
        <StatTile
          label={t(K("activeConnections"))}
          value={formatNumber(listener.active)}
        />
        <StatTile
          label={t(K("addressRanges"))}
          value={String(addresses.total)}
          hint={addressHint}
        />
        <StatTile
          label={t(K("relayed"))}
          value={formatNumber(stats.relayed)}
          hint={relayedHint}
        />
      </Grid>
      {showCounters && (
        <Typography variant="caption" sx={{ color: colors.text.secondary }}>
          {t(K("counters"), {
            failedOpen: formatNumber(stats.failed_open),
            dialFailed: formatNumber(stats.dial_failed),
            dropped: formatNumber(stats.dropped),
          })}
        </Typography>
      )}
    </Stack>
  );
};

const BridgeWarnings = ({ data }: { data: TelegramBridgeStatus }) => {
  const { t } = useTranslation();
  const { tproxy, listener, addresses } = data;
  const tproxyMissing = tproxy.checked && !tproxy.available;
  const tproxyDetail =
    tproxy.packages.length > 0
      ? t(K("tproxyPackages"), { packages: tproxy.packages.join(", ") })
      : tproxy.missing.length > 0
        ? t(K("tproxyMissing"), { missing: tproxy.missing.join(", ") })
        : "";
  const source = t(K(`source.${addresses.source}`), {
    defaultValue: addresses.source,
  });

  return (
    <>
      {tproxyMissing && (
        <B4Alert severity="error">
          {t(K("tproxyUnavailable"))}
          {tproxyDetail && ` ${tproxyDetail}`}
        </B4Alert>
      )}
      {data.skip_setup && (
        <B4Alert severity="warning">
          <Trans
            i18nKey={K("skipSetup")}
            components={{
              a: <Link component={RouterLink} to="/settings/general" />,
            }}
          />
        </B4Alert>
      )}
      {listener.error && !listener.running && (
        <B4Alert severity="error">
          {t(K("listenerError"), {
            port: listener.port,
            error: listener.error,
          })}
        </B4Alert>
      )}
      {listener.running && !listener.v6 && listener.v6_error && (
        <B4Alert severity="info">
          {t(K("listenerV6Error"), { error: listener.v6_error })}
        </B4Alert>
      )}
      {addresses.last_error && (
        <B4Alert severity="info">
          {t(K("downloadFailed"), { error: addresses.last_error, source })}
        </B4Alert>
      )}
      {data.queue_mode === "tun" && (
        <B4Alert severity="info">{t(K("tunUnverified"))}</B4Alert>
      )}
      {data.legacy_sets.length > 0 && (
        <B4Alert severity="info">
          {t(K("legacySets"))}{" "}
          {data.legacy_sets.map((s, i) => (
            <Fragment key={s.id}>
              {i > 0 && ", "}
              <Link
                component={RouterLink}
                to={`/sets/${encodeURIComponent(s.id)}`}
              >
                {s.name || s.id}
              </Link>
              {!s.enabled && ` (${t(K("legacySetOff"))})`}
            </Fragment>
          ))}
        </B4Alert>
      )}
    </>
  );
};

export const MTProtoBridgeCard = ({
  config,
  savedEnabled,
  onChange,
}: MTProtoBridgeCardProps) => {
  const { t } = useTranslation();
  const { showSuccess, showError } = useSnackbar();
  const enabled = config.system.mtproto?.bridge?.enabled ?? false;

  const status = useTelegramBridgeStatus();
  const check = useCheckTelegramBridge();
  const refresh = useRefreshTelegramAddresses();
  const busy = check.isPending || refresh.isPending;

  const data = status.data;
  const dirty = savedEnabled !== enabled;
  const live = !!data && data.enabled && enabled && !dirty;
  const working =
    !!data &&
    data.rule_installed &&
    data.listener.running &&
    !data.rule_shadowed_by;

  const notWorkingReason = (() => {
    if (!data) return "";
    if (!data.listener.running) {
      return data.listener.error || t(K("reasonListener"));
    }
    if (data.tproxy.checked && !data.tproxy.available) {
      return t(K("tproxyUnavailable"));
    }
    if (data.rule_installed && data.rule_shadowed_by) {
      const tables = config.system.tables;
      const monitorOff =
        !!tables?.skip_setup ||
        (tables?.monitor_interval === 0 && data.queue_mode !== "tun");
      return t(K(monitorOff ? "reasonShadowedNoMonitor" : "reasonShadowed"), {
        rule: data.rule_shadowed_by,
      });
    }
    return t(K("reasonRule"));
  })();

  const headerStatus = (() => {
    if (dirty) {
      return (
        <Tooltip title={t(K("saveToApplyHint"))}>
          <Chip
            size="small"
            variant="outlined"
            color="warning"
            label={t(K("saveToApply"))}
          />
        </Tooltip>
      );
    }
    if (!live) return null;
    return (
      <Tooltip title={working ? t(K("workingHint")) : notWorkingReason}>
        <Chip
          size="small"
          color={working ? "success" : "error"}
          label={working ? t(K("working")) : t(K("notWorking"))}
          sx={{ maxWidth: 280 }}
        />
      </Tooltip>
    );
  })();

  const handleCheck = () => {
    check.mutate(undefined, {
      onError: (e) => showError(describeApiError(e)),
    });
  };

  const handleRefresh = () => {
    refresh.mutate(undefined, {
      onSuccess: (res) => {
        if (res.addresses.last_error) {
          showError(
            t(K("downloadFailed"), {
              error: res.addresses.last_error,
              source: t(K(`source.${res.addresses.source}`), {
                defaultValue: res.addresses.source,
              }),
            }),
          );
        } else {
          showSuccess(t(K("refreshDone"), { total: res.addresses.total }));
        }
      },
      onError: (e) => showError(describeApiError(e)),
    });
  };

  return (
    <B4IntegrationCard
      icon={<NetworkIcon />}
      title={t(K("title"))}
      description={t(K("description"))}
      status={headerStatus}
      enabled={enabled}
      onToggle={(checked) => onChange("system.mtproto.bridge.enabled", checked)}
      toggleLabel={t(K("enable"))}
    >
      {data && <BridgeWarnings data={data} />}

      <Box
        sx={{
          border: `1px solid ${colors.border.light}`,
          borderRadius: `${radiusPx.md}px`,
          bgcolor: colors.background.dark,
          p: "14px 15px",
        }}
      >
        <Stack spacing={1.5}>
          <Stack
            direction="row"
            alignItems="center"
            justifyContent="space-between"
            spacing={1}
            useFlexGap
            flexWrap="wrap"
          >
            <Typography
              sx={{
                fontFamily: fonts.mono,
                fontSize: typography.sizes.xs,
                letterSpacing: typography.tracking.wide,
                textTransform: "uppercase",
                color: colors.text.secondary,
              }}
            >
              {t(K("statusTitle"))}
            </Typography>
            <Stack direction="row" spacing={1} useFlexGap flexWrap="wrap">
              <Button
                size="small"
                variant="outlined"
                startIcon={
                  check.isPending ? (
                    <CircularProgress size={14} color="inherit" />
                  ) : (
                    <RefreshIcon fontSize="small" />
                  )
                }
                disabled={busy}
                onClick={handleCheck}
              >
                {t(K("checkAgain"))}
              </Button>
              <Button
                size="small"
                variant="outlined"
                startIcon={
                  refresh.isPending ? (
                    <CircularProgress size={14} color="inherit" />
                  ) : (
                    <CloudDownloadIcon fontSize="small" />
                  )
                }
                disabled={busy}
                onClick={handleRefresh}
              >
                {t(K("refreshAddresses"))}
              </Button>
            </Stack>
          </Stack>

          {status.isLoading && !data && (
            <Stack direction="row" spacing={1} alignItems="center">
              <CircularProgress size={14} sx={{ color: colors.secondary }} />
              <Typography variant="body2" sx={{ color: colors.text.secondary }}>
                {t("core.loading")}
              </Typography>
            </Stack>
          )}
          {status.isError && !data && (
            <B4Alert severity="error">
              {t(K("statusError"), { error: describeApiError(status.error) })}
            </B4Alert>
          )}
          {dirty && (
            <Typography variant="body2" sx={{ color: colors.text.secondary }}>
              {t(K("saveToApplyHint"))}
            </Typography>
          )}
          {live && data && <BridgeStats data={data} />}
        </Stack>
      </Box>

      <Grid container>
        <B4Hint>{t(K("udpNote"))}</B4Hint>
      </Grid>
    </B4IntegrationCard>
  );
};
