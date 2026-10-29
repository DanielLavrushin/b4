import { useTranslation } from "react-i18next";
import { Link, Typography } from "@mui/material";
import { IpIcon, OpenInNewIcon } from "@b4.icons";
import { B4NumberField, B4Section, B4Switch } from "@b4.elements";
import { B4Config } from "@models/config";

interface IPHealthSettingsProps {
  config: B4Config;
  onChange: (
    field: string,
    value: number | boolean | string | string[],
  ) => void;
}

const HOUR_SEC = 3600;
const DAY_SEC = 86400;

const DEFAULTS = {
  retest_interval_sec: 300,
  bans_off: false,
  ban_ttl_sec: 86400,
  repeat_ban_ttl_sec: 604800,
};

const orDefault = (value: number | undefined, fallback: number): number =>
  value !== undefined && value > 0 ? value : fallback;

export const IPHealthSettings = ({
  config,
  onChange,
}: IPHealthSettingsProps) => {
  const { t } = useTranslation();
  const ipHealth = { ...DEFAULTS, ...(config.system.ip_health ?? {}) };
  const bansOn = !ipHealth.bans_off;
  const banTtl = orDefault(ipHealth.ban_ttl_sec, DEFAULTS.ban_ttl_sec);
  const repeatBanTtl = orDefault(
    ipHealth.repeat_ban_ttl_sec,
    DEFAULTS.repeat_ban_ttl_sec,
  );

  return (
    <B4Section title={t("settings.IPHealth.title")} icon={<IpIcon />}>
      <B4NumberField
        value={ipHealth.retest_interval_sec}
        min={30}
        max={3600}
        path="system.ip_health.retest_interval_sec"
        onChange={(value) =>
          onChange("system.ip_health.retest_interval_sec", value)
        }
        helperText={t("settings.IPHealth.retestIntervalHelper")}
        aiTopic="system.ip_health.retest_interval_sec"
      />
      <B4Switch
        label={t("settings.IPHealth.rememberBans")}
        checked={bansOn}
        path="system.ip_health.bans_off"
        onChange={(checked: boolean) =>
          onChange("system.ip_health.bans_off", !checked)
        }
        description={t("settings.IPHealth.rememberBansDesc")}
        aiTopic="system.ip_health.bans_off"
        aiContext={{ bans_off: ipHealth.bans_off }}
      />
      <B4NumberField
        value={Math.round(banTtl / HOUR_SEC)}
        min={1}
        max={720}
        disabled={!bansOn}
        path="system.ip_health.ban_ttl_sec"
        onChange={(value) =>
          onChange("system.ip_health.ban_ttl_sec", value * HOUR_SEC)
        }
        helperText={t("settings.IPHealth.banTtlHelper")}
        aiTopic="system.ip_health.ban_ttl_sec"
        aiContext={{ ban_ttl_sec: banTtl }}
      />
      <B4NumberField
        value={Math.round(repeatBanTtl / DAY_SEC)}
        min={1}
        max={90}
        disabled={!bansOn}
        path="system.ip_health.repeat_ban_ttl_sec"
        onChange={(value) =>
          onChange("system.ip_health.repeat_ban_ttl_sec", value * DAY_SEC)
        }
        helperText={t("settings.IPHealth.repeatBanTtlHelper")}
        aiTopic="system.ip_health.repeat_ban_ttl_sec"
        aiContext={{ repeat_ban_ttl_sec: repeatBanTtl }}
      />
      <Typography variant="caption" sx={{ px: 0.5 }}>
        {t("settings.IPHealth.credit")}{" "}
        <Link
          href="https://github.com/belotserkovtsev/Ladon"
          target="_blank"
          rel="noreferrer"
          sx={{ display: "inline-flex", alignItems: "center", gap: 0.25 }}
        >
          Ladon
          <OpenInNewIcon sx={{ fontSize: 12 }} />
        </Link>
      </Typography>
    </B4Section>
  );
};

export default IPHealthSettings;
