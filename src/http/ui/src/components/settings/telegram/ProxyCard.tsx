import { useTranslation } from "react-i18next";
import { Grid } from "@mui/material";
import { TelegramIcon } from "@b4.icons";
import {
  B4Advanced,
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

const DEFAULT_MAX_CONNECTIONS = 2048;
const DEFAULT_TCP_USER_TIMEOUT = 120;
const DEFAULT_IDLE_TIMEOUT = 300;

const ADVANCED_PATHS = [
  "system.mtproto.max_connections",
  "system.mtproto.tcp_user_timeout_sec",
  "system.mtproto.idle_timeout_sec",
];

export const ProxyCard = ({ config, onChange }: ProxyCardProps) => {
  const { t } = useTranslation();
  const mtproto = config.system.mtproto;
  const enabled = mtproto?.enabled ?? false;
  const publicPort = mtproto?.public_port ?? 0;
  const maxConnections = mtproto?.max_connections || DEFAULT_MAX_CONNECTIONS;
  const tcpUserTimeout =
    mtproto?.tcp_user_timeout_sec || DEFAULT_TCP_USER_TIMEOUT;
  const idleTimeout = mtproto?.idle_timeout_sec || DEFAULT_IDLE_TIMEOUT;
  const customized = [
    maxConnections !== DEFAULT_MAX_CONNECTIONS,
    tcpUserTimeout !== DEFAULT_TCP_USER_TIMEOUT,
    idleTimeout !== DEFAULT_IDLE_TIMEOUT,
  ].filter(Boolean).length;

  return (
    <B4IntegrationCard
      icon={<TelegramIcon />}
      title={t("settings.MTProto.title")}
      description={t("settings.MTProto.serverDesc")}
      info={t("settings.MTProto.serverInfo")}
      learnMore="telegram/mtproto-proxy"
      enabled={enabled}
      onToggle={(checked) => onChange("system.mtproto.enabled", checked)}
      togglePath="system.mtproto.enabled"
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
            value={mtproto?.fake_sni || "storage.googleapis.com"}
            path="system.mtproto.fake_sni"
            onChange={(e) =>
              onChange("system.mtproto.fake_sni", e.target.value)
            }
            helperText={t("settings.MTProto.fakeSNIHelp")}
          />
        </Grid>
        <Grid size={{ xs: 12, md: 8 }}>
          <B4TextField
            value={mtproto?.public_host || ""}
            path="system.mtproto.public_host"
            onChange={(e) =>
              onChange("system.mtproto.public_host", e.target.value)
            }
            placeholder="proxy.example.org"
            helperText={t("settings.MTProto.publicHostHelp")}
            info={t("settings.MTProto.publicHostInfo")}
            learnMore="telegram/mtproto-proxy"
            aiTopic="system.mtproto.public_host"
            selectOnFocus
          />
        </Grid>
        <Grid size={{ xs: 12, md: 4 }}>
          <B4TextField
            value={publicPort > 0 ? String(publicPort) : ""}
            path="system.mtproto.public_port"
            onChange={(e) => {
              const digits = e.target.value.replaceAll(/\D/g, "");
              const n = digits ? Math.min(Number(digits), 65535) : 0;
              if (n !== publicPort) onChange("system.mtproto.public_port", n);
            }}
            placeholder={String(mtproto?.port || 3128)}
            inputMode="numeric"
            helperText={t("settings.MTProto.publicPortHelp")}
            info={t("settings.MTProto.publicPortInfo")}
            learnMore="telegram/mtproto-proxy"
            aiTopic="system.mtproto.public_port"
          />
        </Grid>
      </Grid>

      <B4Advanced paths={ADVANCED_PATHS} customized={customized}>
        <Grid container spacing={2}>
          <Grid size={{ xs: 12, md: 4 }}>
            <B4NumberField
              value={maxConnections}
              path="system.mtproto.max_connections"
              onChange={(n) => onChange("system.mtproto.max_connections", n)}
              min={16}
              max={100000}
              helperText={t("settings.MTProto.maxConnectionsHelp")}
              info={t("settings.MTProto.maxConnectionsInfo")}
              learnMore="settings/mtproto"
            />
          </Grid>
          <Grid size={{ xs: 12, md: 4 }}>
            <B4NumberField
              value={tcpUserTimeout}
              path="system.mtproto.tcp_user_timeout_sec"
              onChange={(n) =>
                onChange("system.mtproto.tcp_user_timeout_sec", n)
              }
              min={-1}
              max={86400}
              helperText={t("settings.MTProto.tcpUserTimeoutHelp")}
              info={t("settings.MTProto.tcpUserTimeoutInfo")}
              learnMore="settings/mtproto"
            />
          </Grid>
          <Grid size={{ xs: 12, md: 4 }}>
            <B4NumberField
              value={idleTimeout}
              path="system.mtproto.idle_timeout_sec"
              onChange={(n) => onChange("system.mtproto.idle_timeout_sec", n)}
              min={-1}
              max={86400}
              helperText={t("settings.MTProto.idleTimeoutHelp")}
              info={t("settings.MTProto.idleTimeoutInfo")}
              learnMore="settings/mtproto"
            />
          </Grid>
        </Grid>
      </B4Advanced>
    </B4IntegrationCard>
  );
};
