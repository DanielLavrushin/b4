import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  Box,
  Button,
  CircularProgress,
  DialogContent,
  Grid,
  Stack,
  Typography,
} from "@mui/material";
import {
  B4Alert,
  B4Dialog,
  B4IntegrationCard,
  B4TextField,
} from "@b4.elements";
import { CommunityIcon, CopyIcon, KeyIcon, RestoreIcon, SyncIcon } from "@b4.icons";
import { colors, fonts, radiusPx, typography } from "@design";
import { B4Config } from "@models/config";
import { useSnackbar } from "@context/SnackbarProvider";
import {
  useHubRecoveryCode,
  useHubRestoreIdentity,
  useHubStatus,
  useHubSync,
} from "@hooks/useHub";
import { copyText, describeApiError, formatTimeAgo } from "@utils";
import { formatDate } from "@components/hub/text";

export interface HubSettingsProps {
  config: B4Config;
  onChange: (field: string, value: boolean | string | string[]) => void;
}

const DEFAULT_HUB_URL = "https://hub.b4core.app";

const parseUrls = (text: string): string[] =>
  text
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean);

interface StatusRowProps {
  label: string;
  value: React.ReactNode;
  hint?: string;
  title?: string;
}

const StatusRow = ({ label, value, hint, title }: StatusRowProps) => (
  <Box
    sx={{
      display: "grid",
      gridTemplateColumns: "140px 1fr",
      gap: 1.5,
      alignItems: "baseline",
    }}
  >
    <Typography
      component="span"
      sx={{
        ...typography.recipes.metricLabel,
        fontWeight: typography.weights.bold,
        color: colors.text.disabled,
      }}
    >
      {label}
    </Typography>
    <Box sx={{ minWidth: 0 }}>
      <Typography
        component="div"
        variant="body2"
        title={title}
        sx={{ color: colors.text.primary, overflowWrap: "anywhere" }}
      >
        {value}
      </Typography>
      {hint && (
        <Typography
          component="div"
          variant="caption"
          sx={{ color: colors.text.secondary, display: "block", mt: 0.25 }}
        >
          {hint}
        </Typography>
      )}
    </Box>
  </Box>
);

export const HubCard = ({ config, onChange }: HubSettingsProps) => {
  const { t } = useTranslation();
  const { showSuccess, showError } = useSnackbar();
  const hub = config.system.hub;
  const enabled = Boolean(hub?.enabled);
  const joinedUrls = (hub?.urls ?? []).join("\n");
  const [urlsText, setUrlsText] = useState(joinedUrls);
  const [recoveryOpen, setRecoveryOpen] = useState(false);
  const [restoreOpen, setRestoreOpen] = useState(false);
  const [restoreCode, setRestoreCode] = useState("");

  const status = useHubStatus(enabled);
  const sync = useHubSync();
  const recovery = useHubRecoveryCode();
  const restore = useHubRestoreIdentity();

  useEffect(() => {
    setUrlsText(joinedUrls);
  }, [joinedUrls]);

  const commitUrls = () => {
    const urls = parseUrls(urlsText);
    if (urls.join("\n") === joinedUrls) {
      setUrlsText(joinedUrls);
      return;
    }
    onChange("system.hub.urls", urls);
  };

  const handleSync = () => {
    sync.mutate(undefined, {
      onSuccess: () => showSuccess(t("hub.status.synced")),
      onError: (e) =>
        showError(t("hub.status.syncFailed", { error: describeApiError(e) })),
    });
  };

  const openRecovery = () => {
    setRecoveryOpen(true);
    recovery.reset();
    recovery.mutate(undefined, {
      onError: (e) =>
        showError(
          t("settings.Hub.identity.recoveryFailed", {
            error: describeApiError(e),
          }),
        ),
    });
  };

  const copyRecovery = async () => {
    const code = recovery.data?.code ?? "";
    if (!code) return;
    if (await copyText(code)) showSuccess(t("core.copied"));
    else showError(t("core.copyFailed"));
  };

  const submitRestore = () => {
    restore.mutate(restoreCode, {
      onSuccess: (res) => {
        showSuccess(
          t("settings.Hub.identity.restored", { key: res.key_id }),
        );
        setRestoreOpen(false);
        setRestoreCode("");
      },
      onError: (e) =>
        showError(
          t("settings.Hub.identity.restoreFailed", {
            error: describeApiError(e),
          }),
        ),
    });
  };

  const data = status.data;

  return (
    <B4IntegrationCard
      icon={<CommunityIcon />}
      title={t("settings.Hub.title")}
      description={t("settings.Hub.description")}
      enabled={enabled}
      onToggle={(checked) => onChange("system.hub.enabled", checked)}
      toggleLabel={t("settings.Hub.enabled")}
    >
      <B4Alert severity="info">{t("settings.Hub.note")}</B4Alert>

      <Grid container spacing={2}>
        <Grid size={{ xs: 12, md: 6 }}>
          <B4TextField
            label={t("settings.Hub.urls")}
            value={urlsText}
            onChange={(e) => setUrlsText(e.target.value)}
            onBlur={commitUrls}
            multiline
            minRows={2}
            placeholder={DEFAULT_HUB_URL}
            helperText={t("settings.Hub.urlsHelp", { url: DEFAULT_HUB_URL })}
          />
        </Grid>
        <Grid size={{ xs: 12, md: 6 }}>
          <B4TextField
            label={t("settings.Hub.publicKey")}
            value={hub?.public_key ?? ""}
            onChange={(e) => onChange("system.hub.public_key", e.target.value)}
            placeholder={t("settings.Hub.publicKeyPlaceholder")}
            helperText={t("settings.Hub.publicKeyHelp")}
          />
        </Grid>
      </Grid>

      <Grid container spacing={2}>
        <Grid size={{ xs: 12, lg: 6 }}>
      <Box
        sx={{
          border: `1px solid ${colors.border.light}`,
          borderRadius: `${radiusPx.md}px`,
          bgcolor: colors.background.dark,
          p: "14px 15px",
          height: "100%",
        }}
      >
        <Stack spacing={1.25}>
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
              {t("settings.Hub.status.title")}
            </Typography>
            <Button
              size="small"
              variant="outlined"
              startIcon={
                sync.isPending ? (
                  <CircularProgress size={14} color="inherit" />
                ) : (
                  <SyncIcon fontSize="small" />
                )
              }
              disabled={sync.isPending || !data?.configured}
              onClick={handleSync}
            >
              {sync.isPending ? t("hub.status.syncing") : t("hub.status.syncNow")}
            </Button>
          </Stack>

          {status.isLoading && !data && (
            <Stack direction="row" spacing={1} alignItems="center">
              <CircularProgress size={14} sx={{ color: colors.secondary }} />
              <Typography variant="body2" sx={{ color: colors.text.secondary }}>
                {t("core.loading")}
              </Typography>
            </Stack>
          )}
          {status.isError && (
            <B4Alert severity="error">
              {t("hub.status.unavailable", {
                error: describeApiError(status.error),
              })}
            </B4Alert>
          )}
          {data && !data.enabled && (
            <Typography variant="body2" sx={{ color: colors.text.secondary }}>
              {t("settings.Hub.status.savedOff")}
            </Typography>
          )}
          {data?.enabled && !data.configured && (
            <B4Alert severity="warning">{t("hub.status.notConfigured")}</B4Alert>
          )}
          {data?.enabled && data.configured && (
            <Stack spacing={1.25}>
              <StatusRow
                label={t("settings.Hub.status.catalogue")}
                value={
                  data.catalogue
                    ? t("settings.Hub.status.catalogueLine", {
                        count: data.catalogue.sets,
                        date: formatDate(data.catalogue.generated_at),
                      })
                    : t("hub.status.noCatalogue")
                }
                title={
                  data.catalogue
                    ? t("settings.Hub.status.catalogueBuild", {
                        epoch: data.catalogue.epoch,
                        seq: data.catalogue.seq,
                      })
                    : undefined
                }
                hint={t("settings.Hub.status.catalogueHint")}
              />
              {data.catalogue && (
                <StatusRow
                  label={t("settings.Hub.status.expires")}
                  value={
                    data.catalogue.expired ? (
                      <Box component="span" sx={{ color: colors.state.warning }}>
                        {t("settings.Hub.status.expiredAt", {
                          date: formatDate(data.catalogue.expires_at),
                        })}
                      </Box>
                    ) : (
                      formatDate(data.catalogue.expires_at)
                    )
                  }
                  hint={t("settings.Hub.status.expiresHint")}
                />
              )}
              <StatusRow
                label={t("settings.Hub.status.lastSync")}
                value={
                  data.last_sync
                    ? `${formatDate(data.last_sync)} (${formatTimeAgo(t, data.last_sync)})`
                    : t("hub.status.neverSynced")
                }
                hint={t("settings.Hub.status.lastSyncHint")}
              />
              {data.last_error && (
                <StatusRow
                  label={t("settings.Hub.status.lastError")}
                  value={
                    <Box component="span" sx={{ color: colors.state.error }}>
                      {data.last_error}
                    </Box>
                  }
                  hint={t("settings.Hub.status.lastErrorHint")}
                />
              )}
              <StatusRow
                label={t("settings.Hub.status.hub")}
                value={[
                  ...(data.urls.length > 0 ? data.urls : [DEFAULT_HUB_URL]),
                  ...data.mirrors,
                ].join(", ")}
                hint={
                  data.mirrors.length > 0
                    ? t("settings.Hub.status.hubHintMirrors", { count: data.mirrors.length })
                    : t("settings.Hub.status.hubHint")
                }
              />
              <StatusRow
                label={t("settings.Hub.status.hubKey")}
                value={
                  data.hub_key
                    ? t(
                        data.hub_key_builtin
                          ? "settings.Hub.status.hubKeyBuiltin"
                          : "settings.Hub.status.hubKeyCustom",
                        { key: data.hub_key },
                      )
                    : t("settings.Hub.status.hubKeyNone")
                }
                hint={t("settings.Hub.status.hubKeyHint")}
              />
              <StatusRow
                label={t("settings.Hub.status.network")}
                value={
                  data.network.asn || data.network.cc
                    ? t("settings.Hub.status.networkLine", {
                        asn: data.network.asn ? `AS${data.network.asn}` : "?",
                        cc: data.network.cc || "?",
                        name: data.network.name ? ` (${data.network.name})` : "",
                      })
                    : t("settings.Hub.status.networkUnknown")
                }
                hint={
                  data.network.asn || data.network.cc
                    ? t(
                        data.network.source === "hub"
                          ? "settings.Hub.status.networkHintHub"
                          : "settings.Hub.status.networkHintDetector",
                      )
                    : t("settings.Hub.status.networkHintUnknown")
                }
              />
              <StatusRow
                label={t("settings.Hub.status.outbox")}
                value={
                  data.outbox > 0
                    ? t("settings.Hub.status.outboxWaiting", { count: data.outbox })
                    : t("settings.Hub.status.outboxNone")
                }
                hint={t("settings.Hub.status.outboxHint")}
              />
            </Stack>
          )}
        </Stack>
      </Box>
        </Grid>
        <Grid size={{ xs: 12, lg: 6 }}>
      <Box
        sx={{
          border: `1px solid ${colors.border.light}`,
          borderRadius: `${radiusPx.md}px`,
          bgcolor: colors.background.dark,
          p: "14px 15px",
          height: "100%",
        }}
      >
        <Stack spacing={1.25}>
          <Typography
            sx={{
              fontFamily: fonts.mono,
              fontSize: typography.sizes.xs,
              letterSpacing: typography.tracking.wide,
              textTransform: "uppercase",
              color: colors.text.secondary,
            }}
          >
            {t("settings.Hub.identity.title")}
          </Typography>
          <Typography variant="body2" sx={{ color: colors.text.secondary }}>
            {t("settings.Hub.identity.help")}
          </Typography>
          <StatusRow
            label={t("settings.Hub.identity.keyId")}
            value={
              data?.key_id ? (
                <Box component="span" sx={{ fontFamily: fonts.mono }}>
                  {data.key_id}
                </Box>
              ) : (
                t("core.unknown")
              )
            }
          />
          <Stack direction="row" spacing={1} useFlexGap flexWrap="wrap">
            <Button
              size="small"
              variant="outlined"
              startIcon={<KeyIcon fontSize="small" />}
              onClick={openRecovery}
              disabled={!data?.enabled}
            >
              {t("settings.Hub.identity.showRecovery")}
            </Button>
            <Button
              size="small"
              variant="outlined"
              startIcon={<RestoreIcon fontSize="small" />}
              onClick={() => setRestoreOpen(true)}
              disabled={!data?.enabled}
            >
              {t("settings.Hub.identity.restore")}
            </Button>
          </Stack>
        </Stack>
      </Box>
        </Grid>
      </Grid>

      <B4Dialog
        title={t("settings.Hub.identity.recoveryTitle")}
        icon={<KeyIcon />}
        open={recoveryOpen}
        onClose={() => setRecoveryOpen(false)}
        maxWidth="sm"
        fullWidth
        actions={
          <>
            <Button onClick={() => setRecoveryOpen(false)}>{t("core.close")}</Button>
            <Box sx={{ flex: 1 }} />
            <Button
              variant="contained"
              startIcon={<CopyIcon />}
              disabled={!recovery.data?.code}
              onClick={() => void copyRecovery()}
            >
              {t("core.copy")}
            </Button>
          </>
        }
      >
        <DialogContent sx={{ p: 0 }}>
          <Stack spacing={2}>
            <B4Alert severity="warning">
              {t("settings.Hub.identity.recoveryWarning")}
            </B4Alert>
            {recovery.isPending && (
              <Stack direction="row" spacing={1} alignItems="center">
                <CircularProgress size={14} sx={{ color: colors.secondary }} />
                <Typography variant="body2" sx={{ color: colors.text.secondary }}>
                  {t("core.loading")}
                </Typography>
              </Stack>
            )}
            {recovery.data?.code && (
              <B4TextField
                label={t("settings.Hub.identity.recoveryCode")}
                value={recovery.data.code}
                selectOnFocus
                multiline
                slotProps={{
                  input: {
                    readOnly: true,
                    sx: { fontFamily: fonts.mono, fontSize: typography.sizes.md },
                  },
                }}
              />
            )}
          </Stack>
        </DialogContent>
      </B4Dialog>

      <B4Dialog
        title={t("settings.Hub.identity.restoreTitle")}
        icon={<RestoreIcon />}
        open={restoreOpen}
        onClose={() => setRestoreOpen(false)}
        maxWidth="sm"
        fullWidth
        actions={
          <>
            <Button onClick={() => setRestoreOpen(false)} disabled={restore.isPending}>
              {t("core.cancel")}
            </Button>
            <Box sx={{ flex: 1 }} />
            <Button
              variant="contained"
              startIcon={
                restore.isPending ? (
                  <CircularProgress size={14} color="inherit" />
                ) : (
                  <RestoreIcon />
                )
              }
              disabled={restore.isPending || !restoreCode.trim()}
              onClick={submitRestore}
            >
              {t("settings.Hub.identity.restoreAction")}
            </Button>
          </>
        }
      >
        <DialogContent sx={{ p: 0 }}>
          <Stack spacing={2}>
            <Typography variant="body2" sx={{ color: colors.text.secondary }}>
              {t("settings.Hub.identity.restoreHelp")}
            </Typography>
            <B4TextField
              label={t("settings.Hub.identity.recoveryCode")}
              value={restoreCode}
              onChange={(e) => setRestoreCode(e.target.value)}
              multiline
              minRows={2}
              autoFocus
              slotProps={{
                input: {
                  sx: { fontFamily: fonts.mono, fontSize: typography.sizes.md },
                },
              }}
            />
          </Stack>
        </DialogContent>
      </B4Dialog>
    </B4IntegrationCard>
  );
};
