import { useTranslation } from "react-i18next";
import { SpeedIcon } from "@b4.icons";
import {
  B4Advanced,
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

const QUEUE_DEFAULTS = {
  start_num: 537,
  mark: 32768,
  threads: 4,
  tcp_conn_bytes_limit: 19,
  udp_conn_bytes_limit: 8,
};

const QUEUE_KEYS = Object.keys(QUEUE_DEFAULTS) as (keyof typeof QUEUE_DEFAULTS)[];
const QUEUE_PATHS = QUEUE_KEYS.map((key) => `queue.${key}`);

export const QueueSettings = ({ config, onChange }: QueueSettingsProps) => {
  const { t } = useTranslation();
  const customized = QUEUE_KEYS.filter(
    (key) => config.queue[key] !== QUEUE_DEFAULTS[key],
  ).length;

  return (
    <B4Section
      title={t("settings.Queue.title")}
      icon={<SpeedIcon />}
      learnMore="settings/core#queue-and-packet-processing"
    >
      <B4Advanced
        paths={QUEUE_PATHS}
        customized={customized}
        summary={t("settings.Queue.summary", {
          queue: config.queue.start_num,
          mark: config.queue.mark,
          threads: config.queue.threads,
          tcp: config.queue.tcp_conn_bytes_limit,
          udp: config.queue.udp_conn_bytes_limit,
        })}
      >
        <B4FormGroup columns={2}>
          <B4NumberField
            value={config.queue.start_num}
            path="queue.start_num"
            onChange={(n) => onChange("queue.start_num", n)}
            min={0}
            helperText={t("settings.Queue.queueStartHelp")}
          />
          <B4NumberField
            value={config.queue.mark}
            path="queue.mark"
            onChange={(n) => onChange("queue.mark", n)}
            min={0}
            helperText={t("settings.Queue.packetMarkHelp")}
          />
          <B4Slider
            value={config.queue.threads}
            path="queue.threads"
            onChange={(value) => onChange("queue.threads", value)}
            min={1}
            max={16}
            step={1}
            helperText={t("settings.Queue.workerThreadsHelp")}
          />
          <B4Slider
            value={config.queue.tcp_conn_bytes_limit}
            path="queue.tcp_conn_bytes_limit"
            onChange={(value) => onChange("queue.tcp_conn_bytes_limit", value)}
            min={1}
            max={100}
            step={1}
            helperText={t("settings.Queue.tcpLimitHelp")}
          />
          <B4Slider
            value={config.queue.udp_conn_bytes_limit}
            path="queue.udp_conn_bytes_limit"
            onChange={(value) => onChange("queue.udp_conn_bytes_limit", value)}
            min={1}
            max={30}
            step={1}
            helperText={t("settings.Queue.udpLimitHelp")}
          />
        </B4FormGroup>
      </B4Advanced>
    </B4Section>
  );
};
