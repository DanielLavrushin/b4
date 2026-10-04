import { useTranslation } from "react-i18next";
import { IpIcon } from "@b4.icons";
import { B4NumberField, B4Section } from "@b4.elements";
import { B4Config } from "@models/config";

interface IPHealthSettingsProps {
  config: B4Config;
  onChange: (
    field: string,
    value: number | boolean | string | string[],
  ) => void;
}

const DEFAULTS = {
  retest_interval_sec: 300,
};

export const IPHealthSettings = ({
  config,
  onChange,
}: IPHealthSettingsProps) => {
  const { t } = useTranslation();
  const ipHealth = { ...DEFAULTS, ...(config.system.ip_health ?? {}) };

  return (
    <B4Section title={t("settings.IPHealth.title")} icon={<IpIcon />}>
      <B4NumberField
        label={t("settings.IPHealth.retestInterval")}
        value={ipHealth.retest_interval_sec}
        min={30}
        max={3600}
        onChange={(value) =>
          onChange("system.ip_health.retest_interval_sec", value)
        }
        helperText={t("settings.IPHealth.retestIntervalHelper")}
        aiTopic="system.ip_health.retest_interval_sec"
      />
    </B4Section>
  );
};

export default IPHealthSettings;
