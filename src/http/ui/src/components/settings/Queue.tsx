import { useTranslation } from "react-i18next";
import { NetworkIcon } from "@b4.icons";
import {
  B4FormGroup,
  B4NumberField,
  B4Section,
  B4Slider,
} from "@b4.elements";
import { B4Config } from "@models/config";
import { SettingsPropHandlerType } from "@models/settings";

interface QueueSettingsProps {
  config: B4Config;
  onChange: (field: string, value: SettingsPropHandlerType) => void;
}

export const QueueSettings = ({ config, onChange }: QueueSettingsProps) => {
  const { t } = useTranslation();

  return (
    <B4Section
      title={t("settings.Queue.title")}
      description={t("settings.Queue.description")}
      icon={<NetworkIcon />}
    >
      <B4FormGroup label={t("settings.Queue.groupLabel")} columns={2}>
        <B4NumberField
          label={t("settings.Queue.queueStart")}
          value={config.queue.start_num}
          onChange={(n) => onChange("queue.start_num", n)}
          min={0}
          helperText={t("settings.Queue.queueStartHelp")}
        />
        <B4NumberField
          label={t("settings.Queue.packetMark")}
          value={config.queue.mark}
          onChange={(n) => onChange("queue.mark", n)}
          min={0}
          helperText={t("settings.Queue.packetMarkHelp")}
        />
        <B4Slider
          label={t("settings.Queue.workerThreads")}
          value={config.queue.threads}
          onChange={(value) => onChange("queue.threads", value)}
          min={1}
          max={16}
          step={1}
          helperText={t("settings.Queue.workerThreadsHelp")}
        />
        <B4Slider
          label={t("settings.Queue.tcpLimit")}
          value={config.queue.tcp_conn_bytes_limit}
          onChange={(value) => onChange("queue.tcp_conn_bytes_limit", value)}
          min={1}
          max={100}
          step={1}
          helperText={t("settings.Queue.tcpLimitHelp")}
        />
        <B4Slider
          label={t("settings.Queue.udpLimit")}
          value={config.queue.udp_conn_bytes_limit}
          onChange={(value) => onChange("queue.udp_conn_bytes_limit", value)}
          min={1}
          max={30}
          step={1}
          helperText={t("settings.Queue.udpLimitHelp")}
        />
      </B4FormGroup>
    </B4Section>
  );
};
