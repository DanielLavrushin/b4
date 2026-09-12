import { Button, CircularProgress, Stack, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";
import { Link } from "react-router";
import { B4Alert } from "@b4.elements";
import { colors } from "@design";
import { HubStatus } from "@models/hub";
import { describeApiError, formatTimeAgo } from "@utils";
import { formatDate } from "./text";

interface StatusPanelProps {
  status: HubStatus | undefined;
  loading: boolean;
  error: unknown;
  syncing: boolean;
  onSync: () => void;
}

export const hubStatusLine = (
  t: ReturnType<typeof useTranslation>["t"],
  status: HubStatus,
): string => {
  const parts: string[] = [];
  if (status.catalogue) {
    parts.push(
      t("hub.status.catalogue", {
        date: formatDate(status.catalogue.generated_at),
      }),
    );
    parts.push(t("hub.status.sets", { count: status.catalogue.sets }));
  } else {
    parts.push(t("hub.status.noCatalogue"));
  }
  parts.push(
    status.last_sync
      ? t("hub.status.lastSync", { ago: formatTimeAgo(t, status.last_sync) })
      : t("hub.status.neverSynced"),
  );
  if (status.network.asn || status.network.cc) {
    parts.push(
      t("hub.status.network", {
        asn: status.network.asn || "?",
        cc: status.network.cc || "?",
      }),
    );
  }
  if (status.outbox > 0) {
    parts.push(t("hub.status.outbox", { count: status.outbox }));
  }
  return parts.join(" · ");
};

export const StatusPanel = ({
  status,
  loading,
  error,
  syncing,
  onSync,
}: StatusPanelProps) => {
  const { t } = useTranslation();

  if (loading && !status) {
    return (
      <Stack direction="row" spacing={1.5} alignItems="center">
        <CircularProgress size={16} sx={{ color: colors.secondary }} />
        <Typography variant="body2" sx={{ color: colors.text.secondary }}>
          {t("core.loading")}
        </Typography>
      </Stack>
    );
  }

  if (!status) {
    return (
      <B4Alert severity="error">
        {t("hub.status.unavailable", { error: describeApiError(error) })}
      </B4Alert>
    );
  }

  const settingsLink = (
    <Button
      component={Link}
      to="/settings/api"
      size="small"
      sx={{ ml: 1, textTransform: "none" }}
    >
      {t("hub.status.openSettings")}
    </Button>
  );

  if (!status.enabled) {
    return (
      <B4Alert severity="warning">
        {t("hub.status.disabled")}
        {settingsLink}
      </B4Alert>
    );
  }

  if (!status.configured) {
    return (
      <B4Alert severity="warning">
        {t("hub.status.notConfigured")}
        {settingsLink}
      </B4Alert>
    );
  }

  return (
    <Stack spacing={1.5}>
      <Stack
        direction="row"
        alignItems="center"
        justifyContent="space-between"
        spacing={2}
        useFlexGap
        flexWrap="wrap"
      >
        <Typography variant="body2" sx={{ color: colors.text.secondary }}>
          {hubStatusLine(t, status)}
        </Typography>
        <Button
          variant="outlined"
          size="small"
          startIcon={
            syncing ? <CircularProgress size={14} color="inherit" /> : undefined
          }
          disabled={syncing}
          onClick={onSync}
          sx={{ whiteSpace: "nowrap" }}
        >
          {syncing ? t("hub.status.syncing") : t("hub.status.syncNow")}
        </Button>
      </Stack>
      {status.catalogue?.expired && (
        <B4Alert severity="warning">
          {t("hub.status.expired", {
            date: formatDate(status.catalogue.expires_at),
          })}
        </B4Alert>
      )}
      {status.last_error && (
        <B4Alert severity="error">
          {t("hub.status.lastError", { error: status.last_error })}
        </B4Alert>
      )}
    </Stack>
  );
};
