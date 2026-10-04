import { useTranslation } from "react-i18next";
import { WebIcon } from "@b4.icons";
import {
  B4Alert,
  B4FormGroup,
  B4FormRow,
  B4Section,
  B4TextField,
} from "@b4.elements";
import { B4Config } from "@models/config";
import { SettingsPropHandlerType } from "@models/settings";
import { ListenerFields } from "./ListenerFields";

interface WebServerSettingsProps {
  config: B4Config;
  onChange: (field: string, value: SettingsPropHandlerType) => void;
}

export const WebServerSettings = ({
  config,
  onChange,
}: WebServerSettingsProps) => {
  const { t } = useTranslation();

  const hasUsername = !!config.system.web_server.username;
  const hasPassword =
    !!config.system.web_server.password ||
    !!config.system.web_server.password_set;
  const authSet = hasUsername && hasPassword;
  const hasTls = !!config.system.web_server.tls_cert;
  const exposed = config.system.web_server.expose ?? false;

  return (
    <B4Section title={t("settings.WebServer.title")} icon={<WebIcon />}>
      <B4FormGroup columns={2}>
        <B4FormRow>
          <ListenerFields
            path="system.web_server"
            listener={config.system.web_server}
            defaultPort={7000}
            skipSetup={config.system.tables.skip_setup}
            onChange={onChange}
            exposeBlocked={
              authSet ? undefined : t("settings.WebServer.exposeNeedsAuth")
            }
            exposeNote={t("settings.WebServer.exposeNote")}
          />
        </B4FormRow>
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
          placeholder={
            config.system.web_server.password_set
              ? t("settings.WebServer.passwordSetPlaceholder")
              : ""
          }
          helperText={t("settings.WebServer.passwordHelp")}
          autoComplete="new-password"
        />
      </B4FormGroup>
      {((hasUsername && !hasPassword) || (!hasUsername && hasPassword)) && (
        <B4Alert severity="warning">
          {t("settings.WebServer.authPartialWarning")}
        </B4Alert>
      )}
      {exposed && !authSet && (
        <B4Alert severity="error">
          {t("settings.WebServer.exposeAuthMissing")}
        </B4Alert>
      )}
      {exposed && !hasTls && (
        <B4Alert severity="warning">
          {t("settings.WebServer.exposeNoTls")}
        </B4Alert>
      )}
      {authSet && !hasTls && !exposed && (
        <B4Alert severity="warning">
          {t("settings.WebServer.authHttpWarning")}
        </B4Alert>
      )}
    </B4Section>
  );
};
