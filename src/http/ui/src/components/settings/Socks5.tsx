import { useTranslation } from "react-i18next";
import { ConnectionIcon } from "@b4.icons";
import {
  B4FormGroup,
  B4NumberField,
  B4Section,
  B4Switch,
  B4TextField,
  B4Alert,
} from "@b4.elements";
import { B4Config } from "@models/config";
import { SettingsPropHandlerType } from "@models/settings";

interface Socks5SettingsProps {
  config: B4Config;
  onChange: (field: string, value: SettingsPropHandlerType) => void;
}

export const Socks5Settings = ({ config, onChange }: Socks5SettingsProps) => {
  const { t } = useTranslation();

  return (
    <B4Section
      title={t("settings.Socks5.title")}
      description={t("settings.Socks5.description")}
      icon={<ConnectionIcon />}
    >
      <B4FormGroup label={t("settings.Socks5.settings")} columns={2}>
        <B4Switch
          label={t("settings.Socks5.enable")}
          checked={config.system.socks5?.enabled ?? false}
          onChange={(checked: boolean) =>
            onChange("system.socks5.enabled", checked)
          }
          description={t("settings.Socks5.enableDesc")}
        />
        <B4TextField
          label={t("settings.Socks5.bindAddress")}
          value={config.system.socks5?.bind_address || "0.0.0.0"}
          onChange={(e) =>
            onChange("system.socks5.bind_address", e.target.value)
          }
          placeholder={t("settings.Socks5.bindAddressPlaceholder")}
          disabled={!config.system.socks5?.enabled}
          helperText={t("settings.Socks5.bindAddressHelp")}
          selectOnFocus
        />
        <B4NumberField
          label={t("settings.Socks5.port")}
          value={config.system.socks5?.port ?? 1080}
          onChange={(n) => onChange("system.socks5.port", n)}
          min={1}
          max={65535}
          disabled={!config.system.socks5?.enabled}
          helperText={t("settings.Socks5.portHelp")}
        />
        <B4TextField
          label={t("settings.Socks5.username")}
          value={config.system.socks5?.username || ""}
          onChange={(e) => onChange("system.socks5.username", e.target.value)}
          disabled={!config.system.socks5?.enabled}
          helperText={t("settings.Socks5.usernameHelp")}
          autoComplete="new-password"
        />
        <B4TextField
          label={t("settings.Socks5.password")}
          type="password"
          value={config.system.socks5?.password || ""}
          onChange={(e) => onChange("system.socks5.password", e.target.value)}
          disabled={!config.system.socks5?.enabled}
          helperText={t("settings.Socks5.passwordHelp")}
          autoComplete="new-password"
        />
        {config.system.socks5?.enabled && (
          <B4Alert severity="info">{t("settings.Socks5.restartNote")}</B4Alert>
        )}
      </B4FormGroup>
    </B4Section>
  );
};
