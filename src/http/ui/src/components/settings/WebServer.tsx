import { useTranslation } from "react-i18next";
import { setLanguage } from "../../i18n";
import { ApiIcon } from "@b4.icons";
import {
  B4Alert,
  B4FormGroup,
  B4NumberField,
  B4Section,
  B4Select,
  B4TextField,
} from "@b4.elements";
import { B4Config } from "@models/config";
import { SettingsPropHandlerType } from "@models/settings";

interface WebServerSettingsProps {
  config: B4Config;
  onChange: (field: string, value: SettingsPropHandlerType) => void;
}

const LANGUAGES = [
  { value: "en", label: "English" },
  { value: "ru", label: "Русский" },
];

export const WebServerSettings = ({
  config,
  onChange,
}: WebServerSettingsProps) => {
  const { t } = useTranslation();

  const handleLanguageChange = (e: { target: { value: string | number } }) => {
    const lang = String(e.target.value);
    onChange("system.web_server.language", lang);
    setLanguage(lang);
  };

  return (
    <B4Section
      title={t("settings.WebServer.title")}
      description={t("settings.WebServer.description")}
      icon={<ApiIcon />}
    >
      <B4FormGroup label={t("settings.WebServer.serverSettings")} columns={2}>
        <B4TextField
          label={t("settings.WebServer.bindAddress")}
          value={config.system.web_server.bind_address || "0.0.0.0"}
          onChange={(e) =>
            onChange("system.web_server.bind_address", e.target.value)
          }
          placeholder={t("settings.WebServer.bindAddressPlaceholder")}
          helperText={t("settings.WebServer.bindAddressHelp")}
          selectOnFocus
        />
        <B4NumberField
          label={t("settings.WebServer.port")}
          value={config.system.web_server.port}
          onChange={(n) => onChange("system.web_server.port", n)}
          min={1}
          max={65535}
          helperText={t("settings.WebServer.portHelp")}
        />
        <B4TextField
          label={t("settings.WebServer.tlsCert")}
          value={config.system.web_server.tls_cert || ""}
          onChange={(e) =>
            onChange("system.web_server.tls_cert", e.target.value)
          }
          placeholder={t("settings.WebServer.tlsCertPlaceholder")}
          helperText={t("settings.WebServer.tlsCertHelp")}
        />
        <B4TextField
          label={t("settings.WebServer.tlsKey")}
          value={config.system.web_server.tls_key || ""}
          onChange={(e) =>
            onChange("system.web_server.tls_key", e.target.value)
          }
          placeholder={t("settings.WebServer.tlsKeyPlaceholder")}
          helperText={t("settings.WebServer.tlsKeyHelp")}
        />
        <B4Select
          label={t("core.language")}
          value={config.system.web_server.language || "en"}
          options={LANGUAGES}
          onChange={handleLanguageChange}
          helperText={t("settings.WebServer.languageHelp")}
        />
      </B4FormGroup>
      <B4FormGroup label={t("settings.WebServer.authentication")} columns={2}>
        <B4TextField
          label={t("settings.WebServer.username")}
          value={config.system.web_server.username || ""}
          onChange={(e) =>
            onChange("system.web_server.username", e.target.value)
          }
          placeholder=""
          helperText={t("settings.WebServer.usernameHelp")}
          autoComplete="new-password"
        />
        <B4TextField
          label={t("settings.WebServer.password")}
          type="password"
          value={config.system.web_server.password || ""}
          onChange={(e) =>
            onChange("system.web_server.password", e.target.value)
          }
          placeholder=""
          helperText={t("settings.WebServer.passwordHelp")}
          autoComplete="new-password"
        />
      </B4FormGroup>
      {((config.system.web_server.username &&
        !config.system.web_server.password) ||
        (!config.system.web_server.username &&
          config.system.web_server.password)) && (
        <B4Alert severity="warning">
          {t("settings.WebServer.authPartialWarning")}
        </B4Alert>
      )}
      {config.system.web_server.username &&
        config.system.web_server.password &&
        !config.system.web_server.tls_cert && (
          <B4Alert severity="warning">
            {t("settings.WebServer.authHttpWarning")}
          </B4Alert>
        )}
    </B4Section>
  );
};
