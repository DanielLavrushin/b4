import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Box, FormHelperText, Typography } from "@mui/material";
import { CategoryIcon, FilterIcon, SwapIcon } from "@b4.icons";
import {
  B4Badge,
  B4FieldMessage,
  B4FormGroup,
  B4FormRow,
  B4Section,
  B4Select,
  B4Slider,
  B4Switch,
  InterfaceChips,
} from "@b4.elements";
import { B4Config } from "@models/config";
import { SettingsPropHandlerType } from "@models/settings";
import { useChangedField } from "@context/ChangedFieldsContext";
import {
  DSCP_MAX_VALUE,
  dscpStampingSets,
  setDscpValues,
  suggestDscpValue,
} from "@utils";

interface FirewallCardProps {
  config: B4Config;
  onChange: (field: string, value: SettingsPropHandlerType) => void;
}

const TUN_MONITOR_MIN_INTERVAL = 10;

export const FirewallRulesSettings = ({
  config,
  onChange,
}: FirewallCardProps) => {
  const { t } = useTranslation();
  const skipTables = config.system.tables.skip_setup;
  const [tunSkipShown] = useState(skipTables);

  return (
    <B4Section
      title={t("settings.Feature.firewallFeatures")}
      icon={<FilterIcon />}
      learnMore="settings/core#firewall-rules"
    >
      {config.queue.mode === "tun" ? (
        <>
          {(skipTables || tunSkipShown) && (
            <B4Switch
              checked={skipTables}
              path="system.tables.skip_setup"
              onChange={(checked: boolean) =>
                onChange("system.tables.skip_setup", checked)
              }
            />
          )}
          <B4Slider
            value={Math.max(
              TUN_MONITOR_MIN_INTERVAL,
              config.system.tables.monitor_interval,
            )}
            path="system.tables.monitor_interval"
            onChange={(value: number) =>
              onChange("system.tables.monitor_interval", value)
            }
            min={TUN_MONITOR_MIN_INTERVAL}
            max={120}
            step={5}
            helperText={t(
              skipTables
                ? "settings.Feature.firewallMonitorTunSkipped"
                : "settings.Feature.firewallMonitorTunHelp",
            )}
            info={
              skipTables
                ? undefined
                : t("settings.Feature.firewallMonitorTunInfo")
            }
          />
        </>
      ) : (
        <B4FormGroup columns={2}>
          <B4FormRow>
            <B4Switch
              checked={skipTables}
              path="system.tables.skip_setup"
              onChange={(checked: boolean) =>
                onChange("system.tables.skip_setup", checked)
              }
              description={t("settings.Feature.skipIptablesDesc")}
              info={t("settings.Feature.skipIptablesInfo")}
            />
          </B4FormRow>
          <B4Select
            value={config.system.tables.engine || "auto"}
            path="system.tables.engine"
            onChange={(e) =>
              onChange(
                "system.tables.engine",
                e.target.value === "auto" ? "" : e.target.value,
              )
            }
            options={[
              { value: "auto", label: t("settings.Feature.engineAuto") },
              { value: "nftables", label: "nftables" },
              { value: "iptables", label: "iptables" },
              { value: "iptables-legacy", label: "iptables-legacy" },
            ]}
            helperText={t("settings.Feature.firewallEngineHelp")}
          />
          <B4Slider
            value={config.system.tables.monitor_interval}
            path="system.tables.monitor_interval"
            onChange={(value: number) =>
              onChange("system.tables.monitor_interval", value)
            }
            min={0}
            max={120}
            step={5}
            disabled={skipTables}
            helperText={t(
              skipTables
                ? "settings.Feature.firewallMonitorSkipped"
                : "settings.Feature.firewallMonitorHelp",
            )}
            alert={
              !skipTables &&
              config.system.tables.monitor_interval <= 0 && (
                <B4FieldMessage>
                  {t("settings.Feature.firewallMonitorWarning")}
                </B4FieldMessage>
              )
            }
          />
        </B4FormGroup>
      )}
    </B4Section>
  );
};


export const NatMasqueradeSettings = ({
  config,
  onChange,
}: FirewallCardProps) => {
  const { t } = useTranslation();
  const skipTables = config.system.tables.skip_setup;
  const masquerade = config.system.tables.masquerade;
  useChangedField(
    "system.tables.masquerade.interfaces",
    t("core.interfaces"),
  );
  const handleMasqueradeToggle = (iface: string) => {
    const current = masquerade.interfaces || [];
    const updated = current.includes(iface)
      ? current.filter((i) => i !== iface)
      : [...current, iface];
    onChange("system.tables.masquerade.interfaces", updated);
  };

  return (
    <B4Section
      title={t("settings.Feature.natMasquerade")}
      icon={<SwapIcon />}
      learnMore="settings/core#nat-masquerade"
    >
      <B4Switch
        checked={masquerade.enabled}
        path="system.tables.masquerade.enabled"
        onChange={(checked: boolean) =>
          onChange("system.tables.masquerade.enabled", checked)
        }
        disabled={skipTables}
        description={t(
          skipTables
            ? "settings.Feature.natMasqueradeSkipped"
            : "settings.Feature.natMasqueradeDesc",
        )}
      />
      {masquerade.enabled && (
        <B4FormGroup
          label={t("core.interfaces")}
          info={t("settings.Feature.masqueradeInterfaceInfo")}
          columns={1}
        >
          <Box>
            <InterfaceChips
              available={config.available_ifaces ?? []}
              selected={masquerade.interfaces || []}
              disabled={skipTables}
              onToggle={handleMasqueradeToggle}
              path="system.tables.masquerade.interfaces"
              details={config.iface_details}
            />
            <FormHelperText sx={{ color: "text.secondary", m: 0, mt: 0.75 }}>
              {t("settings.Feature.masqueradeInterfaceDesc")}
            </FormHelperText>
          </Box>
        </B4FormGroup>
      )}
    </B4Section>
  );
};

export const DscpSettings = ({ config, onChange }: FirewallCardProps) => {
  const { t } = useTranslation();
  const skipTables = config.system.tables.skip_setup;
  const dscp = config.system.tables.dscp ?? {
    enabled: false,
    value: 0,
    interfaces: [],
  };
  const dscpInterfaces = dscp.interfaces || [];
  const stampingSets = dscpStampingSets(config);
  useChangedField(
    "system.tables.dscp.interfaces",
    t("core.interfaces"),
  );

  const handleDscpInterfaceToggle = (iface: string) => {
    const updated = dscpInterfaces.includes(iface)
      ? dscpInterfaces.filter((i) => i !== iface)
      : [...dscpInterfaces, iface];
    onChange("system.tables.dscp.interfaces", updated);
  };

  return (
    <B4Section
      title={t("settings.Feature.dscpSettings")}
      icon={<CategoryIcon />}
      learnMore="settings/core#dscp"
    >
      <B4Switch
        checked={dscp.enabled}
        path="system.tables.dscp.enabled"
        onChange={(checked: boolean) => {
          onChange("system.tables.dscp.enabled", checked);
          if (checked && !dscp.value) {
            onChange(
              "system.tables.dscp.value",
              suggestDscpValue(setDscpValues(config.sets)),
            );
          }
        }}
        disabled={skipTables}
        description={t(
          skipTables
            ? "settings.Feature.dscpSkipped"
            : "settings.Feature.dscpDesc",
        )}
        info={t("settings.Feature.dscpInfo")}
        aiTopic="system.tables.dscp.enabled"
      />
      {dscp.enabled && (
        <B4Slider
          value={dscp.value}
          path="system.tables.dscp.value"
          onChange={(value: number) =>
            onChange("system.tables.dscp.value", value)
          }
          min={0}
          max={DSCP_MAX_VALUE}
          step={1}
          disabled={skipTables}
          helperText={t("settings.Feature.dscpValueHelp")}
          info={t("settings.Feature.dscpValueInfo")}
          aiTopic="system.tables.dscp.value"
        />
      )}
      {stampingSets.length > 0 && (
        <Box>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
            {t("settings.Feature.dscpSetsList")}
          </Typography>
          <Box sx={{ display: "flex", flexWrap: "wrap", gap: 0.5 }}>
            {stampingSets.map((set) => (
              <B4Badge
                key={set.id}
                label={`${set.name || set.id}: ${set.dscp?.value ?? 0}`}
                variant="outlined"
                color="secondary"
              />
            ))}
          </Box>
        </Box>
      )}
      {(dscp.enabled || stampingSets.length > 0) && (
        <B4FormGroup
          label={t("core.interfaces")}
          info={t("settings.Feature.dscpInterfacesInfo")}
          aiTopic="system.tables.dscp.interfaces"
          columns={1}
        >
          <Box>
            <InterfaceChips
              available={config.available_ifaces ?? []}
              selected={dscpInterfaces}
              disabled={skipTables}
              onToggle={handleDscpInterfaceToggle}
              path="system.tables.dscp.interfaces"
              details={config.iface_details}
            />
            <FormHelperText sx={{ color: "text.secondary", m: 0, mt: 0.75 }}>
              {t("settings.Feature.dscpInterfacesDesc")}
            </FormHelperText>
          </Box>
        </B4FormGroup>
      )}
    </B4Section>
  );
};
