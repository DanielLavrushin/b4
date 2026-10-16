import { useEffect, useState } from "react";
import { B4SetConfig, QueueConfig } from "@models/config";
import { B4Tabs, B4Tab, B4TabPanel, B4Section } from "@b4.elements";
import { TcpIcon, FragIcon, FakingIcon, CoreIcon } from "@b4.icons";
import { TcpGeneral } from "./TcpGeneral";
import { TcpSplitting } from "./TcpSplitting";
import { TcpFaking } from "./TcpFaking";
import { useTranslation } from "react-i18next";
import { useChangedFields } from "@context/ChangedFieldsContext";
const TCP_SUB_INDEX: Record<string, number> = {
  general: 0,
  splitting: 1,
  faking: 2,
};
export const TCP_SUB_TABS: { labelKey: string; prefixes: string[] }[] = [
  { labelKey: "sets.tcp.tabs.general", prefixes: ["tcp.conn_bytes_limit", "tcp.dport_filter", "tcp.drop_sack", "tcp.http_methodeol", "tcp.duplicate", "tcp.ip_block_detect", "tcp.rst_protection", "tcp.seg2delay", "tcp.seg2delay_max", "mss_clamp"] },
  { labelKey: "sets.tcp.tabs.splitting", prefixes: ["fragmentation"] },
  { labelKey: "sets.tcp.tabs.faking", prefixes: ["faking", "tcp.syn_fake", "tcp.syn_fake_len", "tcp.syn_ttl", "tcp.desync", "tcp.win", "tcp.incoming"] },
];

interface TcpTabContainerProps {
  initialSub?: string;
  config: B4SetConfig;
  queue: QueueConfig;
  onChange: (
    field: string,
    value: string | number | boolean | string[] | number[],
  ) => void;
}

enum TCP_TABS {
  GENERAL = 0,
  SPLITTING,
  FAKING,
}

export const TcpTabContainer = ({
  initialSub,
  config,
  queue,
  onChange,
}: TcpTabContainerProps) => {
  const { t } = useTranslation();
  const [activeTab, setActiveTab] = useState<TCP_TABS>(
    (TCP_SUB_INDEX[initialSub ?? ""] ?? TCP_TABS.GENERAL),
  );

  useEffect(() => {
    const index = TCP_SUB_INDEX[initialSub ?? ""];
    if (index !== undefined) setActiveTab(index);
  }, [initialSub]);
  const { changedPaths } = useChangedFields();
  const hasSubChanges = (prefixes: string[]): boolean =>
    prefixes.some((prefix) => {
      for (const path of changedPaths) {
        if (path === prefix || path.startsWith(`${prefix}.`)) return true;
      }
      return false;
    });

  return (
    <B4Section
      title={t("sets.tcp.sectionTitle")}
      description={t("sets.tcp.sectionDescription")}
      icon={<TcpIcon />}
    >
      <B4Tabs
        value={activeTab}
        onChange={(_, v: number) => {
          setActiveTab(v);
        }}
      >
        <B4Tab icon={<CoreIcon />} label={t("sets.tcp.tabs.general")} inline hasChanges={hasSubChanges(TCP_SUB_TABS[0].prefixes)} />
        <B4Tab
          icon={<FragIcon />}
          label={t("sets.tcp.tabs.splitting")}
          inline
          hasChanges={hasSubChanges(TCP_SUB_TABS[1].prefixes)}
        />
        <B4Tab icon={<FakingIcon />} label={t("sets.tcp.tabs.faking")} inline hasChanges={hasSubChanges(TCP_SUB_TABS[2].prefixes)} />
      </B4Tabs>

      <B4TabPanel value={activeTab} index={TCP_TABS.GENERAL} idPrefix="tcp-tab">
        <TcpGeneral config={config} queue={queue} onChange={onChange} />
      </B4TabPanel>

      <B4TabPanel value={activeTab} index={TCP_TABS.SPLITTING} idPrefix="tcp-tab">
        <TcpSplitting config={config} onChange={onChange} />
      </B4TabPanel>

      <B4TabPanel value={activeTab} index={TCP_TABS.FAKING} idPrefix="tcp-tab">
        <TcpFaking config={config} onChange={onChange} />
      </B4TabPanel>
    </B4Section>
  );
};
