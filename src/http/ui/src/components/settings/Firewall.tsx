import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Box, Typography } from "@mui/material";
import { CategoryIcon, FilterIcon, SwapIcon } from "@b4.icons";
import {
  B4Alert,
  B4Badge,
  B4FormGroup,
  B4FormRow,
  B4Section,
  B4Select,
  B4Slider,
  B4Switch,
} from "@b4.elements";
import { B4Config } from "@models/config";
import { SettingsPropHandlerType } from "@models/settings";

interface FirewallCardProps {
  config: B4Config;
  onChange: (field: string, value: SettingsPropHandlerType) => void;
}

const TUN_MONITOR_MIN_INTERVAL = 10;
const DSCP_SUGGESTED_VALUE = 7;
const DSCP_MAX_VALUE = 63;

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
    >
      {config.queue.mode === "tun" ? (
        <>
          {(skipTables || tunSkipShown) && (
            <B4Switch
              label={t("settings.Feature.skipIptables")}
              checked={skipTables}
              onChange={(checked: boolean) =>
                onChange("system.tables.skip_setup", checked)
              }
            />
          )}
          <B4Slider
            label={t("settings.Feature.firewallMonitorInterval")}
            value={Math.max(
              TUN_MONITOR_MIN_INTERVAL,
              config.system.tables.monitor_interval,
            )}
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
          />
        </>
      ) : (
        <B4FormGroup columns={2}>
          <B4FormRow>
            <B4Switch
              label={t("settings.Feature.skipIptables")}
              checked={skipTables}
              onChange={(checked: boolean) =>
                onChange("system.tables.skip_setup", checked)
              }
              description={t("settings.Feature.skipIptablesDesc")}
            />
          </B4FormRow>
          <B4Select
            label={t("settings.Feature.firewallEngine")}
            value={config.system.tables.engine || "auto"}
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
            label={t("settings.Feature.firewallMonitorInterval")}
            value={config.system.tables.monitor_interval}
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
                <B4Alert severity="warning">
                  {t("settings.Feature.firewallMonitorWarning")}
                </B4Alert>
              )
            }
          />
        </B4FormGroup>
      )}
    </B4Section>
  );
};

interface InterfaceChipsProps {
  available: string[];
  selected: string[];
  disabled: boolean;
  onToggle: (iface: string) => void;
}

const InterfaceChips = ({
  available,
  selected,
  disabled,
  onToggle,
}: InterfaceChipsProps) => {
  const { t } = useTranslation();
  const toggle = (iface: string) =>
    disabled ? undefined : () => onToggle(iface);

  return (
    <>
      <Box sx={{ display: "flex", flexWrap: "wrap", gap: 0.5 }}>
        {selected
          .filter((iface) => !available.includes(iface))
          .map((iface) => (
            <B4Badge
              key={iface}
              label={`${iface} (${t("settings.Feature.missingIface")})`}
              onClick={toggle(iface)}
              variant="filled"
              color="error"
            />
          ))}
        {available.map((iface) => (
          <B4Badge
            key={iface}
            label={iface}
            onClick={toggle(iface)}
            variant={selected.includes(iface) ? "filled" : "outlined"}
            color="primary"
          />
        ))}
      </Box>
      {available.length === 0 && (
        <B4Alert severity="warning" sx={{ mt: 1 }}>
          {t("settings.Feature.noInterfacesDetected")}
        </B4Alert>
      )}
    </>
  );
};

export const NatMasqueradeSettings = ({
  config,
  onChange,
}: FirewallCardProps) => {
  const { t } = useTranslation();
  const skipTables = config.system.tables.skip_setup;
  const masquerade = config.system.tables.masquerade;

  const handleMasqueradeToggle = (iface: string) => {
    const current = masquerade.interfaces || [];
    const updated = current.includes(iface)
      ? current.filter((i) => i !== iface)
      : [...current, iface];
    onChange("system.tables.masquerade.interfaces", updated);
  };

  return (
    <B4Section title={t("settings.Feature.natMasquerade")} icon={<SwapIcon />}>
      <B4Switch
        label={t("settings.Feature.natMasqueradeEnable")}
        checked={masquerade.enabled}
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
        <Box>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
            {t("settings.Feature.masqueradeInterfaceDesc")}
          </Typography>
          <InterfaceChips
            available={config.available_ifaces ?? []}
            selected={masquerade.interfaces || []}
            disabled={skipTables}
            onToggle={handleMasqueradeToggle}
          />
        </Box>
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
    >
      <B4Switch
        label={t("settings.Feature.dscp")}
        checked={dscp.enabled}
        onChange={(checked: boolean) => {
          onChange("system.tables.dscp.enabled", checked);
          if (checked && !dscp.value) {
            onChange("system.tables.dscp.value", DSCP_SUGGESTED_VALUE);
          }
        }}
        disabled={skipTables}
        description={t(
          skipTables
            ? "settings.Feature.dscpSkipped"
            : "settings.Feature.dscpDesc",
        )}
        aiTopic="system.tables.dscp.enabled"
      />
      {dscp.enabled && (
        <>
          <B4Slider
            label={t("settings.Feature.dscpValue")}
            value={dscp.value}
            onChange={(value: number) =>
              onChange("system.tables.dscp.value", value)
            }
            min={0}
            max={DSCP_MAX_VALUE}
            step={1}
            disabled={skipTables}
            helperText={t("settings.Feature.dscpValueHelp")}
            aiTopic="system.tables.dscp.value"
          />
          <Box>
            <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
              {t("settings.Feature.dscpInterfacesDesc")}
            </Typography>
            <InterfaceChips
              available={config.available_ifaces ?? []}
              selected={dscpInterfaces}
              disabled={skipTables}
              onToggle={handleDscpInterfaceToggle}
            />
          </Box>
        </>
      )}
    </B4Section>
  );
};
