import { useTranslation } from "react-i18next";
import { Grid } from "@mui/material";
import { DomainIcon } from "@b4.icons";
import {
  B4Accordion,
  B4FieldMessage,
  B4IntegrationCard,
  B4TextField,
} from "@b4.elements";
import { B4Config } from "@models/config";
import { SettingsPropHandlerType } from "@models/settings";
import { ExposeSwitch } from "../ListenerFields";
import { WebProxyPagePanel } from "./WebProxyPagePanel";

interface WebCarrierCardProps {
  config: B4Config;
  onChange: (field: string, value: SettingsPropHandlerType) => void;
}

export const WebCarrierCard = ({ config, onChange }: WebCarrierCardProps) => {
  const { t } = useTranslation();
  const webProxy = config.system.mtproto?.web_proxy;
  const hostname = (webProxy?.hostname || "").trim();
  const enabled = webProxy?.enabled ?? false;
  const port = webProxy?.port ?? 0;
  const exposed = webProxy?.expose ?? false;
  const webServerPort = config.system.web_server?.port ?? 0;
  const portClash = port > 0 && port === webServerPort;
  const ownCert = !!webProxy?.tls_cert;
  const ownKey = !!webProxy?.tls_key;
  const webCert = !!config.system.web_server?.tls_cert;
  const webKey = !!config.system.web_server?.tls_key;
  let tlsSummary: string | undefined;
  if (ownCert && ownKey) {
    tlsSummary = t("settings.MTProto.webProxyTlsSummaryOwn");
  } else if (!ownCert && !ownKey && webCert && webKey) {
    tlsSummary = t("settings.MTProto.webProxyTlsSummaryWeb");
  } else if (!ownCert && !ownKey && !webCert && !webKey) {
    tlsSummary = t("settings.MTProto.webProxyTlsSummaryNone");
  }

  return (
    <B4IntegrationCard
      icon={<DomainIcon />}
      title={t("settings.MTProto.webProxyTitle")}
      description={t("settings.MTProto.webProxyDesc")}
      info={t("settings.MTProto.webProxyInfo")}
      learnMore="telegram/web-proxy"
      enabled={enabled}
      onToggle={(checked) => {
        onChange("system.mtproto.web_proxy.enabled", checked);
        if (checked && !hostname && port === 0) {
          onChange("system.mtproto.web_proxy.port", 443);
        }
      }}
      togglePath="system.mtproto.web_proxy.enabled"
      toggleLabel={t("settings.MTProto.webProxyEnable")}
    >
      <Grid container spacing={2}>
        <Grid size={{ xs: 12, md: 8 }}>
          <B4TextField
            value={webProxy?.hostname || ""}
            path="system.mtproto.web_proxy.hostname"
            onChange={(e) =>
              onChange("system.mtproto.web_proxy.hostname", e.target.value)
            }
            placeholder="relay.example.org"
            error={!hostname}
            helperText={t(
              hostname
                ? "settings.MTProto.webProxyHostnameHelp"
                : "settings.MTProto.webProxyNoHost",
            )}
            info={t("settings.MTProto.webProxyHostnameInfo")}
            learnMore="telegram/web-proxy"
            selectOnFocus
          />
        </Grid>
        <Grid size={{ xs: 12, md: 4 }}>
          <B4TextField
            value={port > 0 ? String(port) : ""}
            path="system.mtproto.web_proxy.port"
            onChange={(e) => {
              const digits = e.target.value.replace(/\D/g, "");
              const n = digits ? Math.min(Number(digits), 65535) : 0;
              if (n !== port) onChange("system.mtproto.web_proxy.port", n);
            }}
            placeholder="443"
            inputMode="numeric"
            error={portClash}
            helperText={t(
              port > 0
                ? "settings.MTProto.webProxyPortOwn"
                : "settings.MTProto.webProxyPortShared",
            )}
            message={
              portClash ? (
                <B4FieldMessage severity="error">
                  {t("settings.MTProto.webProxyPortClash")}
                </B4FieldMessage>
              ) : undefined
            }
            info={t("settings.MTProto.webProxyPortInfo")}
            learnMore="telegram/web-proxy"
          />
        </Grid>
        {(port > 0 || exposed) && (
          <Grid size={{ xs: 12 }}>
            <ExposeSwitch
              checked={exposed}
              path="system.mtproto.web_proxy.expose"
              onChange={(checked) =>
                onChange("system.mtproto.web_proxy.expose", checked)
              }
              skipSetup={config.system.tables.skip_setup}
            />
          </Grid>
        )}
      </Grid>
      {port > 0 && (
        <B4Accordion
          title={t("settings.MTProto.webProxyTlsTitle")}
          summary={tlsSummary}
        >
          <Grid container spacing={2}>
            <Grid size={{ xs: 12, md: 6 }}>
              <B4TextField
                value={webProxy?.tls_cert || ""}
                path="system.mtproto.web_proxy.tls_cert"
                onChange={(e) =>
                  onChange("system.mtproto.web_proxy.tls_cert", e.target.value)
                }
                placeholder="/etc/b4/relay.crt"
                helperText={t("settings.MTProto.webProxyTlsCertHelp")}
                info={t("settings.MTProto.webProxyTlsInfo")}
                learnMore="telegram/web-proxy"
              />
            </Grid>
            <Grid size={{ xs: 12, md: 6 }}>
              <B4TextField
                value={webProxy?.tls_key || ""}
                path="system.mtproto.web_proxy.tls_key"
                onChange={(e) =>
                  onChange("system.mtproto.web_proxy.tls_key", e.target.value)
                }
                placeholder="/etc/b4/relay.key"
                helperText={t("settings.MTProto.webProxyTlsKeyHelp")}
              />
            </Grid>
          </Grid>
        </B4Accordion>
      )}
      <WebProxyPagePanel enabled={enabled} hostname={hostname} />
    </B4IntegrationCard>
  );
};
