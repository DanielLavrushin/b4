import { useState, useEffect } from "react";
import { Box, Stack } from "@mui/material";
import { DomainIcon, IpIcon, DeviceIcon } from "@b4.icons";
import { B4Section, B4Tabs, B4Tab, B4TabPanel } from "@b4.elements";
import { B4SetConfig, GeoConfig } from "@models/config";
import { useDevices } from "@b4.devices";
import { useGeoCategories } from "@hooks/useGeoCategories";
import { useSetOverlaps } from "@hooks/useSets";
import { overlapsForSet } from "@models/sets";
import { useTranslation } from "react-i18next";
import { useChangedFields } from "@context/ChangedFieldsContext";
import { hasTargets } from "./facets";
import { SetStats } from "./Manager";
import { DomainsTab } from "./targets/DomainsTab";
import { IpsTab } from "./targets/IpsTab";
import { DevicesTab } from "./targets/DevicesTab";
import { OtherSetsTargets } from "./targets/overlap";

export type { OtherSetsTargets };
const TARGET_SUB_INDEX: Record<string, number> = {
  domains: 0,
  ips: 1,
  asns: 1,
  devices: 2,
};
export const TARGET_SUB_TABS: { labelKey: string; prefixes: string[] }[] = [
  { labelKey: "sets.targets.tabs.domains", prefixes: ["targets.sni_domains", "targets.domain_only", "targets.tls", "targets.ip_version", "targets.geosite_categories", "targets.geosite_exclude"] },
  { labelKey: "sets.targets.tabs.ips", prefixes: ["targets.ip", "targets.ip_priority", "targets.geoip_categories", "targets.geoip_exclude", "targets.asns"] },
  { labelKey: "sets.targets.tabs.sourceDevices", prefixes: ["targets.source_devices", "targets.source_devices_exclude"] },
];

interface TargetSettingsProps {
  initialSub?: string;
  config: B4SetConfig;
  geo: GeoConfig;
  stats?: SetStats;
  otherSetsTargets?: OtherSetsTargets;
  ipv4?: boolean;
  ipv6?: boolean;
  onChange: (
    field: string,
    value: string | string[] | boolean | Record<string, string[]>,
  ) => void;
}

enum TARGET_TABS {
  DOMAINS = 0,
  IPS,
  DEVICES,
}

export const TargetSettings = ({
  initialSub,
  config,
  onChange,
  geo,
  stats,
  otherSetsTargets,
  ipv4,
  ipv6,
}: TargetSettingsProps) => {
  const { t } = useTranslation();
  const [activeTab, setActiveTab] = useState<TARGET_TABS>(
    TARGET_SUB_INDEX[initialSub ?? ""] ?? TARGET_TABS.DOMAINS,
  );

  const [scrollToAsn, setScrollToAsn] = useState(initialSub === "asns");

  useEffect(() => {
    const index = TARGET_SUB_INDEX[initialSub ?? ""];
    if (index !== undefined) setActiveTab(index);
    setScrollToAsn(initialSub === "asns");
  }, [initialSub]);
  const {
    devices,
    loading: devicesLoading,
    available: devicesAvailable,
    loadDevices,
  } = useDevices();
  const { categories: geositeCategories, loading: geositeLoading } =
    useGeoCategories("/api/geosite", !!geo.sitedat_path);
  const { categories: geoipCategories, loading: geoipLoading } =
    useGeoCategories("/api/geoip", !!geo.ipdat_path);

  useEffect(() => {
    loadDevices().catch(() => {});
  }, [loadDevices]);
  const { data: overlaps } = useSetOverlaps(config.id);

  const selectedSourceDevices: string[] = config.targets.source_devices ?? [];
  const { changedPaths } = useChangedFields();
  const hasSubChanges = (prefixes: string[]): boolean =>
    prefixes.some((prefix) => {
      for (const path of changedPaths) {
        if (path === prefix || path.startsWith(`${prefix}.`)) return true;
      }
      return false;
    });
  return (
    <Stack spacing={3}>
      <B4Section
        title={t("sets.targets.sectionTitle")}
        description={t("sets.targets.sectionDescription")}
        icon={<DomainIcon />}
      >
        <Box sx={{ borderBottom: 1, borderColor: "divider", mb: 0 }}>
          <B4Tabs
            value={activeTab}
            onChange={(_, newValue: number) => {
              setActiveTab(newValue);
              setScrollToAsn(false);
            }}
          >
            <B4Tab
              icon={<DomainIcon />}
              label={t("sets.targets.tabs.domains")}
              inline
              hasChanges={hasSubChanges(TARGET_SUB_TABS[0].prefixes)}
            />
            <B4Tab
              icon={<IpIcon />}
              label={t("sets.targets.tabs.ips")}
              inline
              hasChanges={hasSubChanges(TARGET_SUB_TABS[1].prefixes)}
            />
            <B4Tab
              icon={<DeviceIcon />}
              label={
                selectedSourceDevices.length > 0
                  ? `${t("sets.targets.tabs.sourceDevices")} (${selectedSourceDevices.length})`
                  : t("sets.targets.tabs.sourceDevices")
              }
              inline
              hasChanges={hasSubChanges(TARGET_SUB_TABS[2].prefixes)}
            />
          </B4Tabs>
        </Box>

        <B4TabPanel
          value={activeTab}
          index={TARGET_TABS.DOMAINS}
          idPrefix="target-tab"
        >
          <DomainsTab
            config={config}
            geo={geo}
            stats={stats}
            ipv4={ipv4}
            ipv6={ipv6}
            otherSetsTargets={otherSetsTargets}
            overlaps={overlapsForSet(overlaps, config.id)}
            geositeCategories={geositeCategories}
            geositeLoading={geositeLoading}
            onChange={onChange}
          />
        </B4TabPanel>

        <B4TabPanel
          value={activeTab}
          index={TARGET_TABS.IPS}
          idPrefix="target-tab"
        >
          <IpsTab
            config={config}
            geo={geo}
            stats={stats}
            otherSetsTargets={otherSetsTargets}
            geoipCategories={geoipCategories}
            geoipLoading={geoipLoading}
            ipv6={ipv6}
            scrollToAsn={scrollToAsn}
            onChange={onChange}
          />
        </B4TabPanel>

        <B4TabPanel
          value={activeTab}
          index={TARGET_TABS.DEVICES}
          idPrefix="target-tab"
        >
          <DevicesTab
            selected={selectedSourceDevices}
            exclude={config.targets.source_devices_exclude ?? false}
            routingEnabled={!!config.routing?.enabled}
            hasDestination={hasTargets(config)}
            devices={devices}
            loading={devicesLoading}
            available={devicesAvailable}
            onRefresh={() => {
              loadDevices().catch(() => {});
            }}
            onChange={(macs) => onChange("targets.source_devices", macs)}
            onExcludeChange={(exclude) =>
              onChange("targets.source_devices_exclude", exclude)
            }
          />
        </B4TabPanel>
      </B4Section>
    </Stack>
  );
};
