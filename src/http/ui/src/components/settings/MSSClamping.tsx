import { Link } from "@mui/material";
import { Link as RouterLink } from "react-router";
import { Trans, useTranslation } from "react-i18next";
import { FragIcon } from "@b4.icons";
import { B4Section, B4Slider, B4Switch, B4Hint } from "@b4.elements";
import { B4Config } from "@models/config";

interface MSSClampingSettingsProps {
  config: B4Config;
  onChange: (
    field: string,
    value: number | boolean | string | string[],
  ) => void;
}

export const MSSClampingSettings = ({
  config,
  onChange,
}: MSSClampingSettingsProps) => {
  const { t } = useTranslation();
  const mss = config.queue.mss_clamp ?? { enabled: false, size: 88 };

  return (
    <B4Section title={t("settings.MSSClamping.title")} icon={<FragIcon />}>
      <B4Switch
        label={t("settings.MSSClamping.enable")}
        checked={mss.enabled}
        onChange={(checked: boolean) =>
          onChange("queue.mss_clamp.enabled", checked)
        }
        description={t("settings.MSSClamping.enableDesc")}
      />
      {mss.enabled && (
        <B4Slider
          label={t("settings.MSSClamping.mssSize")}
          value={mss.size}
          onChange={(value: number) => onChange("queue.mss_clamp.size", value)}
          min={10}
          max={1460}
          step={1}
          helperText={t("settings.MSSClamping.mssSizeHelp")}
        />
      )}
      <B4Hint>
        <Trans
          i18nKey="settings.MSSClamping.info"
          components={{
            a: <Link component={RouterLink} to="/settings/general/devices" />,
          }}
        />
      </B4Hint>
    </B4Section>
  );
};
