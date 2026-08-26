import { useEffect, useState } from "react";
import { B4Section, B4Tab, B4TabPanel, B4Tabs } from "@b4.elements";
import { DnsIcon, RoutingIcon } from "@b4.icons";
import { B4SetConfig } from "@models/config";
import { useTranslation } from "react-i18next";
import { DnsRedirect } from "./routing/DnsRedirect";
import { TrafficRouting } from "./routing/TrafficRouting";

enum ROUTING_TABS {
  DNS = 0,
  ROUTING,
}

const ROUTING_SUB_INDEX: Record<string, number> = {
  dns: 0,
  traffic: 1,
};

interface RoutingSettingsProps {
  initialSub?: string;
  set: B4SetConfig;
  ipv6: boolean;
  availableIfaces: string[];
  tunnelIfaces?: string[];
  onChange: (
    field: string,
    value:
      | string
      | number
      | boolean
      | string[]
      | number[]
      | Record<string, string[]>
      | null
      | undefined,
  ) => void;
}

export const RoutingSettings = ({
  initialSub,
  set,
  ipv6,
  availableIfaces,
  tunnelIfaces,
  onChange,
}: RoutingSettingsProps) => {
  const { t } = useTranslation();
  const [activeTab, setActiveTab] = useState<ROUTING_TABS>(
    (ROUTING_SUB_INDEX[initialSub ?? ""] ?? ROUTING_TABS.DNS),
  );

  useEffect(() => {
    const index = ROUTING_SUB_INDEX[initialSub ?? ""];
    if (index !== undefined) setActiveTab(index);
  }, [initialSub]);

  return (
    <B4Section
      title={t("sets.routing.sectionTitle")}
      description={t("sets.routing.sectionDescription")}
      icon={<RoutingIcon />}
    >
      <B4Tabs
        value={activeTab}
        onChange={(_, v: number) => {
          setActiveTab(v);
        }}
      >
        <B4Tab icon={<DnsIcon />} label={t("sets.dns.sectionTitle")} inline />
        <B4Tab
          icon={<RoutingIcon />}
          label={t("sets.routing.trafficRouting")}
          inline
        />
      </B4Tabs>

      <B4TabPanel value={activeTab} index={ROUTING_TABS.DNS} idPrefix="routing-tab">
        <DnsRedirect config={set} ipv6={ipv6} onChange={onChange} />
      </B4TabPanel>

      <B4TabPanel value={activeTab} index={ROUTING_TABS.ROUTING} idPrefix="routing-tab">
        <TrafficRouting
          config={set}
          availableIfaces={availableIfaces}
          tunnelIfaces={tunnelIfaces}
          onChange={onChange}
        />
      </B4TabPanel>
    </B4Section>
  );
};
