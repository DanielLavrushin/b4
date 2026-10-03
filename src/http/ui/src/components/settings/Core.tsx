import { useMemo, useState } from "react";
import { Box, Button, Divider } from "@mui/material";
import { useTranslation } from "react-i18next";
import { setLanguage } from "../../i18n";
import { ControlIcon, InfoIcon, LogsIcon, RestartIcon } from "@b4.icons";
import { B4Section, B4Select, B4Switch, B4TextField } from "@b4.elements";
import { B4Config, LogLevel } from "@models/config";
import { SettingsPropHandlerType } from "@models/settings";
import { RestartDialog } from "./RestartDialog";
import { SystemInfoDialog } from "./SystemInfoDialog";

interface CoreCardProps {
  config: B4Config;
  onChange: (field: string, value: SettingsPropHandlerType) => void;
}

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

const LANGUAGES = [
  { value: "en", label: "English" },
  { value: "ru", label: "Русский" },
];

export const ServiceSettings = ({ config, onChange }: CoreCardProps) => {
  const { t } = useTranslation();

  const [showRestartDialog, setShowRestartDialog] = useState(false);
  const [showSysInfoDialog, setShowSysInfoDialog] = useState(false);

  const TIMEZONES = useMemo(
    () => [
      { value: "", label: t("settings.Logging.timezoneAuto") },
      ...ZONE_ENTRIES,
    ],
    [t],
  );

  const handleLanguageChange = (e: { target: { value: string | number } }) => {
    const lang = String(e.target.value);
    onChange("system.web_server.language", lang);
    setLanguage(lang);
  };

  return (
    <B4Section title={t("settings.Logging.title")} icon={<ControlIcon />}>
      <Box sx={{ display: "flex", flexWrap: "wrap", gap: 1 }}>
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
      </Box>
      <Divider />
      <B4Select
        label={t("core.language")}
        value={config.system.web_server.language || "en"}
        options={LANGUAGES}
        onChange={handleLanguageChange}
      />
      <B4Select
        label={t("settings.Logging.timezone")}
        value={config.system.timezone ?? ""}
        options={TIMEZONES}
        onChange={(e) => onChange("system.timezone", String(e.target.value))}
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
      <B4TextField
        label={t("settings.Logging.updateMirrors")}
        value={(config.system.update?.mirrors ?? []).join(", ")}
        onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
          onChange(
            "system.update.mirrors",
            e.target.value
              .split(/[\s,]+/)
              .map((m) => m.trim())
              .filter(Boolean),
          )
        }
        placeholder={t("settings.Logging.updateMirrorsPlaceholder")}
        helperText={t("settings.Logging.updateMirrorsHelp")}
      />

      <RestartDialog
        open={showRestartDialog}
        onClose={() => setShowRestartDialog(false)}
      />
      <SystemInfoDialog
        open={showSysInfoDialog}
        onClose={() => setShowSysInfoDialog(false)}
      />
    </B4Section>
  );
};

export const LoggingSettings = ({ config, onChange }: CoreCardProps) => {
  const { t } = useTranslation();

  const LOG_LEVELS: Array<{ value: LogLevel; label: string }> = [
    { value: LogLevel.ERROR, label: t("settings.Logging.levelError") },
    { value: LogLevel.INFO, label: t("settings.Logging.levelInfo") },
    { value: LogLevel.TRACE, label: t("settings.Logging.levelTrace") },
    { value: LogLevel.DEBUG, label: t("settings.Logging.levelDebug") },
  ];

  return (
    <B4Section title={t("settings.Logging.loggingTitle")} icon={<LogsIcon />}>
      <B4Select
        label={t("settings.Logging.logLevel")}
        value={config.system.logging.level}
        options={LOG_LEVELS}
        onChange={(e) =>
          onChange("system.logging.level", Number(e.target.value))
        }
      />
      <B4TextField
        label={t("settings.Logging.logDirectory")}
        value={config.system.logging.directory}
        onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
          onChange("system.logging.directory", e.target.value)
        }
        placeholder={t("settings.Logging.logDirectoryPlaceholder")}
        helperText={t("settings.Logging.logDirectoryHelp")}
      />
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
      />
    </B4Section>
  );
};
