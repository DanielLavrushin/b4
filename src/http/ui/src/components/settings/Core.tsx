import { useMemo, useState } from "react";
import {
  Box,
  Button,
  DialogContent,
  DialogContentText,
  Divider,
  Grid,
  Stack,
} from "@mui/material";
import { useTranslation } from "react-i18next";
import {
  ControlIcon,
  RestartIcon,
  InfoIcon,
  RestoreIcon,
} from "@b4.icons";
import {
  B4Dialog,
  B4Section,
  B4Select,
  B4Switch,
  B4TextField,
} from "@b4.elements";
import { B4Config, LogLevel } from "@models/config";
import { SettingsPropHandlerType } from "@models/settings";
import { configApi } from "@b4.settings";
import { useSnackbar } from "@context/SnackbarProvider";
import { RestartDialog } from "./RestartDialog";
import { SystemInfoDialog } from "./SystemInfoDialog";

interface LoggingSettingsProps {
  config: B4Config;
  onChange: (field: string, value: SettingsPropHandlerType) => void;
}

// Timezone list is locale-independent, compute once at module level
const ZONE_ENTRIES: { value: string; label: string }[] = (() => {
  try {
    return Intl.supportedValuesOf("timeZone").map((tz) => {
      const offset =
        new Intl.DateTimeFormat("en", {
          timeZone: tz,
          timeZoneName: "shortOffset",
        })
          .formatToParts()
          .find((p) => p.type === "timeZoneName")?.value ?? "";
      return { value: tz, label: `${tz} (${offset})` };
    });
  } catch {
    return [{ value: "UTC", label: "UTC" }];
  }
})();

export const LoggingSettings = ({ config, onChange }: LoggingSettingsProps) => {
  const { t } = useTranslation();
  const { showError, showSuccess } = useSnackbar();

  const [showRestartDialog, setShowRestartDialog] = useState(false);
  const [showSysInfoDialog, setShowSysInfoDialog] = useState(false);
  const [showResetDialog, setShowResetDialog] = useState(false);
  const [resetting, setResetting] = useState(false);

  const handleResetConfirm = async () => {
    try {
      setResetting(true);
      await configApi.reset();
      showSuccess(t("settings.Control.resetSuccess"));
      setTimeout(() => globalThis.window.location.reload(), 800);
    } catch (error) {
      showError(
        error instanceof Error ? error.message : t("settings.Control.resetError"),
      );
      setResetting(false);
      setShowResetDialog(false);
    }
  };

  const TIMEZONES = useMemo(
    () => [
      { value: "", label: t("settings.Logging.timezoneAuto") },
      ...ZONE_ENTRIES,
    ],
    [t],
  );

  const LOG_LEVELS: Array<{ value: LogLevel; label: string }> = [
    { value: LogLevel.ERROR, label: t("settings.Logging.levelError") },
    { value: LogLevel.INFO, label: t("settings.Logging.levelInfo") },
    { value: LogLevel.TRACE, label: t("settings.Logging.levelTrace") },
    { value: LogLevel.DEBUG, label: t("settings.Logging.levelDebug") },
  ];

  return (
    <B4Section
      title={t("settings.Logging.title")}
      description={t("settings.Logging.description")}
      icon={<ControlIcon />}
    >
      <Box sx={{ display: "flex", flexWrap: "wrap", gap: 1, mb: 2 }}>
        <Button
          size="small"
          variant="outlined"
          startIcon={<RestartIcon />}
          onClick={() => setShowRestartDialog(true)}
        >
          {t("settings.Control.restartService")}
        </Button>
        <Button
          size="small"
          variant="outlined"
          startIcon={<InfoIcon />}
          onClick={() => setShowSysInfoDialog(true)}
        >
          {t("settings.Control.systemInfo")}
        </Button>
        <Button
          size="small"
          variant="outlined"
          color="warning"
          startIcon={<RestoreIcon />}
          onClick={() => setShowResetDialog(true)}
        >
          {t("settings.Control.resetConfig")}
        </Button>
      </Box>
      <Divider sx={{ mb: 2 }} />

      <Grid container spacing={2}>
        <Grid size={{ xs: 12, md: 6 }}>
          <Stack spacing={2}>
            <B4Select
              label={t("settings.Logging.logLevel")}
              value={config.system.logging.level}
              options={LOG_LEVELS}
              onChange={(e) =>
                onChange("system.logging.level", Number(e.target.value))
              }
              helperText={t("settings.Logging.logLevelHelp")}
            />
            <B4TextField
              label={t("settings.Logging.errorFilePath")}
              value={config.system.logging.error_file}
              onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
                onChange("system.logging.error_file", e.target.value)
              }
              placeholder={t("settings.Logging.errorFilePathPlaceholder")}
              helperText={t("settings.Logging.errorFilePathHelp")}
            />
            <B4Select
              label={t("settings.Logging.timezone")}
              value={config.system.timezone ?? ""}
              options={TIMEZONES}
              onChange={(e) =>
                onChange("system.timezone", String(e.target.value))
              }
              helperText={t("settings.Logging.timezoneHelp")}
            />
            <B4TextField
              label={t("settings.Logging.memoryLimit")}
              value={config.system.memory_limit ?? ""}
              onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
                onChange("system.memory_limit", e.target.value)
              }
              placeholder={t("settings.Logging.memoryLimitPlaceholder")}
              helperText={t("settings.Logging.memoryLimitHelp")}
            />
          </Stack>
        </Grid>
        <Grid size={{ xs: 12, md: 6 }}>
          <Stack spacing={2}>
            <B4Switch
              label={t("settings.Logging.instantFlush")}
              checked={config?.system?.logging?.instaflush}
              onChange={(checked: boolean) =>
                onChange("system.logging.instaflush", Boolean(checked))
              }
              description={t("settings.Logging.instantFlushDesc")}
            />
            <B4Switch
              label={t("settings.Logging.syslog")}
              checked={config?.system?.logging?.syslog}
              onChange={(checked: boolean) =>
                onChange("system.logging.syslog", Boolean(checked))
              }
              description={t("settings.Logging.syslogDesc")}
            />
          </Stack>
        </Grid>
      </Grid>

      <RestartDialog
        open={showRestartDialog}
        onClose={() => setShowRestartDialog(false)}
      />
      <SystemInfoDialog
        open={showSysInfoDialog}
        onClose={() => setShowSysInfoDialog(false)}
      />
      <B4Dialog
        title={t("settings.Control.resetConfig")}
        open={showResetDialog}
        onClose={() => !resetting && setShowResetDialog(false)}
        actions={
          <>
            <Button
              onClick={() => setShowResetDialog(false)}
              disabled={resetting}
            >
              {t("core.cancel")}
            </Button>
            <Button
              onClick={() => {
                void handleResetConfirm();
              }}
              variant="contained"
              color="warning"
              disabled={resetting}
            >
              {resetting
                ? t("core.saving")
                : t("settings.Control.resetConfig")}
            </Button>
          </>
        }
      >
        <DialogContent>
          <DialogContentText>
            {t("settings.Control.resetConfirm")}
          </DialogContentText>
        </DialogContent>
      </B4Dialog>
    </B4Section>
  );
};
