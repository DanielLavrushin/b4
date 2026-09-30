import { useTranslation } from "react-i18next";
import { Grid } from "@mui/material";
import { TelegramIcon } from "@b4.icons";
import {
  B4Accordion,
  B4IntegrationCard,
  B4NumberField,
  B4TextField,
} from "@b4.elements";
import { B4Config } from "@models/config";
import { SettingsPropHandlerType } from "@models/settings";
import { ListenerFields } from "../ListenerFields";

interface ProxyCardProps {
  config: B4Config;
  onChange: (field: string, value: SettingsPropHandlerType) => void;
}

export const ProxyCard = ({ config, onChange }: ProxyCardProps) => {
  const { t } = useTranslation();
  const mtproto = config.system.mtproto;
  const enabled = mtproto?.enabled ?? false;

  return (
    <B4IntegrationCard
      icon={<TelegramIcon />}
      title={t("settings.MTProto.title")}
      description={t("settings.MTProto.serverDesc")}
      enabled={enabled}
      onToggle={(checked) => onChange("system.mtproto.enabled", checked)}
      toggleLabel={t("settings.MTProto.enable")}
    >
      <Grid container spacing={2}>
        <Grid size={{ xs: 12 }}>
          <ListenerFields
            path="system.mtproto"
            listener={mtproto}
            defaultPort={3128}
            skipSetup={config.system.tables.skip_setup}
            onChange={onChange}
          />
        </Grid>
        <Grid size={{ xs: 12 }}>
          <B4TextField
            label={t("settings.MTProto.fakeSNI")}
            value={mtproto?.fake_sni || "storage.googleapis.com"}
            onChange={(e) =>
              onChange("system.mtproto.fake_sni", e.target.value)
            }
            helperText={t("settings.MTProto.fakeSNIHelp")}
          />
        </Grid>
      </Grid>

      <B4Accordion title={t("settings.MTProto.advanced")}>
        <Grid container spacing={2}>
          <Grid size={{ xs: 12, md: 4 }}>
            <B4NumberField
              label={t("settings.MTProto.maxConnections")}
              value={mtproto?.max_connections || 2048}
              onChange={(n) => onChange("system.mtproto.max_connections", n)}
              min={16}
              max={100000}
              helperText={t("settings.MTProto.maxConnectionsHelp")}
            />
          </Grid>
          <Grid size={{ xs: 12, md: 4 }}>
            <B4NumberField
              label={t("settings.MTProto.tcpUserTimeout")}
              value={mtproto?.tcp_user_timeout_sec || 120}
              onChange={(n) =>
                onChange("system.mtproto.tcp_user_timeout_sec", n)
              }
              min={-1}
              max={86400}
              helperText={t("settings.MTProto.tcpUserTimeoutHelp")}
            />
          </Grid>
          <Grid size={{ xs: 12, md: 4 }}>
            <B4NumberField
              label={t("settings.MTProto.idleTimeout")}
              value={mtproto?.idle_timeout_sec || 300}
              onChange={(n) => onChange("system.mtproto.idle_timeout_sec", n)}
              min={-1}
              max={86400}
              helperText={t("settings.MTProto.idleTimeoutHelp")}
            />
          </Grid>
        </Grid>
      </B4Accordion>
    </B4IntegrationCard>
  );
};
