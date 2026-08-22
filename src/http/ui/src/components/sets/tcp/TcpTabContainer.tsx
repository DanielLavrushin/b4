import { useState } from "react";
import { B4SetConfig, QueueConfig } from "@models/config";
import { B4Tabs, B4Tab, B4TabPanel, B4Section } from "@b4.elements";
import { TcpIcon, FragIcon, FakingIcon, CoreIcon } from "@b4.icons";
import { TcpGeneral } from "./TcpGeneral";
import { TcpSplitting } from "./TcpSplitting";
import { TcpFaking } from "./TcpFaking";
import { useTranslation } from "react-i18next";

interface TcpTabContainerProps {
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
  config,
  queue,
  onChange,
}: TcpTabContainerProps) => {
  const { t } = useTranslation();
  const [activeTab, setActiveTab] = useState<TCP_TABS>(TCP_TABS.GENERAL);

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
        <B4Tab icon={<CoreIcon />} label={t("sets.tcp.tabs.general")} inline />
        <B4Tab
          icon={<FragIcon />}
          label={t("sets.tcp.tabs.splitting")}
          inline
        />
        <B4Tab icon={<FakingIcon />} label={t("sets.tcp.tabs.faking")} inline />
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
