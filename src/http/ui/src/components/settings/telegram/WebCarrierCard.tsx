import { useTranslation } from "react-i18next";
import { Grid, Typography } from "@mui/material";
import { DomainIcon } from "@b4.icons";
import {
  B4Accordion,
  B4Hint,
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

  return (
    <B4IntegrationCard
      icon={<DomainIcon />}
      title={t("settings.MTProto.webProxyTitle")}
      description={t("settings.MTProto.webProxyDesc")}
      enabled={enabled}
      onToggle={(checked) => {
        onChange("system.mtproto.web_proxy.enabled", checked);
        if (checked && !hostname && port === 0) {
          onChange("system.mtproto.web_proxy.port", 443);
        }
      }}
      toggleLabel={t("settings.MTProto.webProxyEnable")}
    >
      <Grid container spacing={2}>
        <Grid size={{ xs: 12, md: 8 }}>
          <B4TextField
            label={t("settings.MTProto.webProxyHostname")}
            value={webProxy?.hostname || ""}
            onChange={(e) =>
              onChange("system.mtproto.web_proxy.hostname", e.target.value)
            }
            placeholder="relay.example.org"
            error={!hostname}
            helperText={
              hostname
                ? t("settings.MTProto.webProxyHostnameHelp")
                : t("settings.MTProto.webProxyNoHost")
            }
            selectOnFocus
          />
        </Grid>
        <Grid size={{ xs: 12, md: 4 }}>
          <B4TextField
            label={t("settings.MTProto.webProxyPort")}
            value={port > 0 ? String(port) : ""}
            onChange={(e) => {
              const digits = e.target.value.replace(/\D/g, "");
              const n = digits ? Math.min(Number(digits), 65535) : 0;
              if (n !== port) onChange("system.mtproto.web_proxy.port", n);
            }}
            placeholder="443"
            inputMode="numeric"
            error={portClash}
            helperText={
              portClash
                ? t("settings.MTProto.webProxyPortClash")
                : port > 0
                  ? t("settings.MTProto.webProxyPortOwn")
                  : t("settings.MTProto.webProxyPortShared")
            }
          />
        </Grid>
        {(port > 0 || exposed) && (
          <Grid size={{ xs: 12 }}>
            <ExposeSwitch
              checked={exposed}
              onChange={(checked) =>
                onChange("system.mtproto.web_proxy.expose", checked)
              }
              skipSetup={config.system.tables.skip_setup}
            />
          </Grid>
        )}
        {port === 0 && (
          <B4Hint>{t("settings.MTProto.webProxyExposeShared")}</B4Hint>
        )}
      </Grid>
      <Grid container>
        <B4Hint>
          {port > 0
            ? t("settings.MTProto.webProxyRequirementsOwnPort")
            : t("settings.MTProto.webProxyRequirements")}
        </B4Hint>
      </Grid>
      {port > 0 && (
        <B4Accordion title={t("settings.MTProto.webProxyTlsTitle")}>
          <Typography
            variant="body2"
            sx={{
              color: "text.secondary",
              mb: 2
            }}>
            {t("settings.MTProto.webProxyTlsDesc")}
          </Typography>
          <Grid container spacing={2}>
            <Grid size={{ xs: 12, md: 6 }}>
              <B4TextField
                label={t("settings.MTProto.webProxyTlsCert")}
                value={webProxy?.tls_cert || ""}
                onChange={(e) =>
                  onChange("system.mtproto.web_proxy.tls_cert", e.target.value)
                }
                placeholder="/etc/b4/relay.crt"
                helperText={t("settings.MTProto.webProxyTlsCertHelp")}
              />
            </Grid>
            <Grid size={{ xs: 12, md: 6 }}>
              <B4TextField
                label={t("settings.MTProto.webProxyTlsKey")}
                value={webProxy?.tls_key || ""}
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
