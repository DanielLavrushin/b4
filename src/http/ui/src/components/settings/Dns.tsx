import { useTranslation } from "react-i18next";
import { DnsIcon } from "@b4.icons";
import {
  B4FormGroup,
  B4Hint,
  B4NumberField,
  B4Section,
  B4Switch,
} from "@b4.elements";
import { B4Config } from "@models/config";

interface DnsSettingsProps {
  config: B4Config;
  onChange: (
    field: string,
    value: number | boolean | string | string[],
  ) => void;
}

const DEFAULTS = {
  tcp_disabled: false,
  tcp_port: 5453,
  query_timeout_sec: 5,
  tcp_idle_sec: 30,
  tcp_io_sec: 10,
  tcp_dial_sec: 5,
};

export const DnsSettings = ({ config, onChange }: DnsSettingsProps) => {
  const { t } = useTranslation();
  const dns = { ...DEFAULTS, ...(config.system.dns ?? {}) };
  const tcpOn = !dns.tcp_disabled;

  return (
    <B4Section
      title={t("settings.Dns.title")}
      description={t("settings.Dns.description")}
      icon={<DnsIcon />}
    >
      <B4Hint>{t("settings.Dns.info")}</B4Hint>

      <B4FormGroup label={t("settings.Dns.tcpGroup")} columns={2}>
        <B4Switch
          label={t("settings.Dns.tcpEnable")}
          checked={tcpOn}
          onChange={(checked: boolean) =>
            onChange("system.dns.tcp_disabled", !checked)
          }
          description={t("settings.Dns.tcpEnableDesc")}
        />
        {tcpOn && (
          <B4NumberField
            label={t("settings.Dns.tcpPort")}
            value={dns.tcp_port}
            onChange={(value: number) => onChange("system.dns.tcp_port", value)}
            min={1}
            max={65535}
            helperText={t("settings.Dns.tcpPortHelp")}
          />
        )}
      </B4FormGroup>

      <B4FormGroup label={t("settings.Dns.timeoutGroup")} columns={2}>
        <B4NumberField
          label={t("settings.Dns.queryTimeout")}
          value={dns.query_timeout_sec}
          onChange={(value: number) =>
            onChange("system.dns.query_timeout_sec", value)
          }
          min={1}
          max={60}
          helperText={t("settings.Dns.queryTimeoutHelp")}
        />
        {tcpOn && (
          <B4NumberField
            label={t("settings.Dns.tcpIdle")}
            value={dns.tcp_idle_sec}
            onChange={(value: number) =>
              onChange("system.dns.tcp_idle_sec", value)
            }
            min={1}
            max={300}
            helperText={t("settings.Dns.tcpIdleHelp")}
          />
        )}
        {tcpOn && (
          <B4NumberField
            label={t("settings.Dns.tcpIo")}
            value={dns.tcp_io_sec}
            onChange={(value: number) =>
              onChange("system.dns.tcp_io_sec", value)
            }
            min={1}
            max={120}
            helperText={t("settings.Dns.tcpIoHelp")}
          />
        )}
        {tcpOn && (
          <B4NumberField
            label={t("settings.Dns.tcpDial")}
            value={dns.tcp_dial_sec}
            onChange={(value: number) =>
              onChange("system.dns.tcp_dial_sec", value)
            }
            min={1}
            max={60}
            helperText={t("settings.Dns.tcpDialHelp")}
          />
        )}
      </B4FormGroup>
    </B4Section>
  );
};
