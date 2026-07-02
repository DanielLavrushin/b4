import { useState, useEffect } from "react";
import { Box, Stack } from "@mui/material";
import { DomainIcon, IpIcon, DeviceIcon } from "@b4.icons";
import { B4Section, B4Tabs, B4Tab, B4TabPanel } from "@b4.elements";
import { B4SetConfig, GeoConfig } from "@models/config";
import { useDevices } from "@b4.devices";
import { useGeoCategories } from "@hooks/useGeoCategories";
import { useTranslation } from "react-i18next";
import { SetStats } from "./Manager";
import { DomainsTab } from "./targets/DomainsTab";
import { IpsTab } from "./targets/IpsTab";
import { DevicesTab } from "./targets/DevicesTab";
import { OtherSetsTargets } from "./targets/overlap";

export type { OtherSetsTargets };

interface TargetSettingsProps {
  config: B4SetConfig;
  geo: GeoConfig;
  stats?: SetStats;
  otherSetsTargets?: OtherSetsTargets;
  ipv4?: boolean;
  ipv6?: boolean;
  onChange: (field: string, value: string | string[] | boolean) => void;
}

enum TARGET_TABS {
  DOMAINS = 0,
  IPS,
  DEVICES,
}

export const TargetSettings = ({
  config,
  onChange,
  geo,
  stats,
  otherSetsTargets,
  ipv4,
  ipv6,
}: TargetSettingsProps) => {
  const { t } = useTranslation();
  const [activeTab, setActiveTab] = useState<TARGET_TABS>(TARGET_TABS.DOMAINS);
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

  const selectedSourceDevices: string[] = config.targets.source_devices ?? [];

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
            onChange={(_, newValue: number) => setActiveTab(newValue)}
          >
            <B4Tab
              icon={<DomainIcon />}
              label={t("sets.targets.tabs.domains")}
              inline
            />
            <B4Tab icon={<IpIcon />} label={t("sets.targets.tabs.ips")} inline />
            <B4Tab
              icon={<DeviceIcon />}
              label={
                selectedSourceDevices.length > 0
                  ? `${t("sets.targets.tabs.sourceDevices")} (${selectedSourceDevices.length})`
                  : t("sets.targets.tabs.sourceDevices")
              }
              inline
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
            otherSetsTargets={otherSetsTargets}
            ipv4={ipv4}
            ipv6={ipv6}
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
