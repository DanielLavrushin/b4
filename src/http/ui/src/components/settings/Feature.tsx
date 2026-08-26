import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { ToggleOnIcon } from "@b4.icons";
import { B4Config } from "@models/config";
import { systemApi } from "@api/settings";
import {
  B4Slider,
  B4FormGroup,
  B4Section,
  B4Switch,
  B4Select,
  B4Alert,
  B4Badge,
  B4TextField,
} from "@b4.elements";
import { Box, Typography } from "@mui/material";
import { SettingsPropHandlerType } from "@models/settings";

interface FeatureSettingsProps {
  config: B4Config;
  onChange: (field: string, value: SettingsPropHandlerType) => void;
}

const IPV6_BYPASS_DISMISS_KEY = "b4_ipv6_bypass_dismissed";

export const FeatureSettings = ({ config, onChange }: FeatureSettingsProps) => {
  const { t } = useTranslation();
  const [ipv6BypassesSets, setIpv6BypassesSets] = useState(false);
  const [ipv6BypassDismissed, setIpv6BypassDismissed] = useState(() => {
    try {
      return localStorage.getItem(IPV6_BYPASS_DISMISS_KEY) === "true";
    } catch {
      return false;
    }
  });

  const ifaceTraffic = config.iface_traffic ?? {};
  const selectedIfaces = config.queue.interfaces ?? [];
  const leaving = (iface: string) => ifaceTraffic[iface]?.leaving ?? 0;
  const trafficTotal = Object.values(ifaceTraffic).reduce(
    (a, c) => a + c.leaving,
    0,
  );
  const trafficSelected = selectedIfaces.reduce(
    (a, iface) => a + leaving(iface),
    0,
  );
  const uncovered = Object.keys(ifaceTraffic)
    .filter((iface) => !selectedIfaces.includes(iface) && leaving(iface) >= 50)
    .sort((a, b) => leaving(b) - leaving(a));
  const uncoveredShare = Math.round(
    ((trafficTotal - trafficSelected) / Math.max(trafficTotal, 1)) * 100,
  );
  const ifaceFilterSeesNothing =
    selectedIfaces.length > 0 && uncovered.length > 0 && trafficSelected === 0;
  const ifaceFilterSeesLittle =
    selectedIfaces.length > 0 && uncovered.length > 0 && trafficSelected > 0;

  useEffect(() => {
    systemApi
      .info()
      .then((info) => setIpv6BypassesSets(!!info?.ipv6_bypasses_sets))
      .catch(() => setIpv6BypassesSets(false));
  }, []);

  const dismissIpv6Bypass = () => {
    try {
      localStorage.setItem(IPV6_BYPASS_DISMISS_KEY, "true");
    } catch (e) {
      console.error("Failed to save the IPv6 bypass notice dismissal:", e);
    }
    setIpv6BypassDismissed(true);
  };

  const showIpv6Bypass =
    ipv6BypassesSets && !config.queue.ipv6 && !ipv6BypassDismissed;

  const handleInterfaceToggle = (iface: string) => {
    const current = config.queue.interfaces || [];
    const updated = current.includes(iface)
      ? current.filter((i) => i !== iface)
      : [...current, iface];
    onChange("queue.interfaces", updated);
  };

  const handleMasqueradeToggle = (iface: string) => {
    const current = config.system.tables.masquerade.interfaces || [];
    const updated = current.includes(iface)
      ? current.filter((i) => i !== iface)
      : [...current, iface];
    onChange("system.tables.masquerade.interfaces", updated);
  };

  const tunOutInterface = config.queue.tun?.out_interface;
  const tunFollowsDefault = !tunOutInterface || tunOutInterface === "auto";

  const skipTables = config.system.tables.skip_setup;

  const masqueradeSwitch = (
    <B4Switch
      label={t("settings.Feature.natMasquerade")}
      checked={config.system.tables.masquerade.enabled}
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
  );

  return (
    <B4Section
      title={t("settings.Feature.title")}
      description={t("settings.Feature.description")}
      icon={<ToggleOnIcon />}
    >
      <B4FormGroup label={t("settings.Feature.protoFeatures")} columns={2}>
        <B4Switch
          label={t("settings.Feature.enableIPv4")}
          checked={config.queue.ipv4}
          onChange={(checked: boolean) => onChange("queue.ipv4", checked)}
          description={t("settings.Feature.enableIPv4Desc")}
        />
        <B4Switch
          label={t("settings.Feature.enableIPv6")}
          checked={config.queue.ipv6}
          onChange={(checked: boolean) => onChange("queue.ipv6", checked)}
          description={t("settings.Feature.enableIPv6Desc")}
        />
      </B4FormGroup>
      {showIpv6Bypass && (
        <B4Alert
          severity="warning"
          noWrapper
          onClose={dismissIpv6Bypass}
          sx={{ mt: 1 }}
        >
          {t("settings.Feature.ipv6BypassWarning")}
        </B4Alert>
      )}
      <B4FormGroup label={t("settings.Feature.engineMode")} columns={1}>
        <B4Select
          label={t("settings.Feature.engineModeLabel")}
          value={config.queue.mode || "nfqueue"}
          onChange={(e) =>
            onChange(
              "queue.mode",
              e.target.value === "nfqueue" ? "" : e.target.value,
            )
          }
          options={[
            {
              value: "nfqueue",
              label: t("settings.Feature.engineModeNfqueue"),
            },
            { value: "tun", label: t("settings.Feature.engineModeTun") },
          ]}
          helperText={t("settings.Feature.engineModeHelp")}
        />
      </B4FormGroup>
      {config.queue.mode === "tun" && (
        <B4FormGroup label={t("settings.Feature.tunSettings")} columns={2}>
          <B4Select
            label={t("settings.Feature.tunOutInterface")}
            value={tunFollowsDefault ? "" : tunOutInterface ?? ""}
            onChange={(e) =>
              onChange("queue.tun.out_interface", e.target.value)
            }
            options={[
              { value: "", label: t("settings.Feature.tunOutInterfaceAuto") },
              ...(config.available_ifaces ?? [])
                .filter((i) => i !== (config.queue.tun?.device_name || "b4tun0"))
                .map((i) => ({
                  value: i,
                  label: i,
                })),
            ]}
            helperText={t("settings.Feature.tunOutInterfaceDesc")}
          />
          <B4TextField
            label={t("settings.Feature.tunOutGateway")}
            value={config.queue.tun?.out_gateway || ""}
            onChange={(e) => onChange("queue.tun.out_gateway", e.target.value)}
            placeholder={t("settings.Feature.tunOutGatewayPlaceholder")}
            disabled={tunFollowsDefault}
            helperText={t(
              tunFollowsDefault
                ? "settings.Feature.tunOutGatewayAuto"
                : "settings.Feature.tunOutGatewayHelp"
            )}
            selectOnFocus
          />
          <B4TextField
            label={t("settings.Feature.tunAddress")}
            value={config.queue.tun?.address || "10.255.0.1/30"}
            onChange={(e) => onChange("queue.tun.address", e.target.value)}
            helperText={t("settings.Feature.tunAddressHelp")}
            selectOnFocus
          />
          <B4TextField
            label={t("settings.Feature.tunDeviceName")}
            value={config.queue.tun?.device_name || "b4tun0"}
            onChange={(e) => onChange("queue.tun.device_name", e.target.value)}
            helperText={t("settings.Feature.tunDeviceNameHelp")}
            selectOnFocus
          />
          {tunFollowsDefault && (
            <B4Alert severity="info">
              {t("settings.Feature.tunOutInterfaceAutoHint")}
            </B4Alert>
          )}
        </B4FormGroup>
      )}
      {config.queue.mode !== "tun" && (
        <B4FormGroup label={t("settings.Feature.firewallFeatures")} columns={2}>
          <B4Switch
            label={t("settings.Feature.skipIptables")}
            checked={skipTables}
            onChange={(checked: boolean) =>
              onChange("system.tables.skip_setup", checked)
            }
            description={t("settings.Feature.skipIptablesDesc")}
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
          {masqueradeSwitch}
        </B4FormGroup>
      )}
      {config.queue.mode === "tun" && (
        <B4FormGroup label={t("settings.Feature.firewallFeatures")} columns={2}>
          {masqueradeSwitch}
        </B4FormGroup>
      )}
      {config.system.tables.masquerade.enabled && (
        <B4FormGroup
          label={t("settings.Feature.masqueradeInterface")}
          columns={1}
        >
          <Box>
            <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
              {t("settings.Feature.masqueradeInterfaceDesc")}
            </Typography>
            <Box sx={{ display: "flex", flexWrap: "wrap", gap: 0.5 }}>
              {(config.available_ifaces ?? []).map((iface) => {
                const isSelected = (
                  config.system.tables.masquerade.interfaces || []
                ).includes(iface);
                return (
                  <B4Badge
                    key={iface}
                    label={iface}
                    onClick={() => handleMasqueradeToggle(iface)}
                    variant={isSelected ? "filled" : "outlined"}
                    color={"primary"}
                  />
                );
              })}
            </Box>
            {(config.available_ifaces ?? []).length === 0 && (
              <B4Alert severity="warning" sx={{ mt: 1 }}>
                {t("settings.Feature.noInterfacesDetected")}
              </B4Alert>
            )}
            {(config.system.tables.masquerade.interfaces || []).length === 0 && (
              <B4Alert severity="info" sx={{ mt: 2 }}>
                {t("settings.Feature.masqueradeAllInterfaces")}
              </B4Alert>
            )}
          </Box>
        </B4FormGroup>
      )}
      {config.queue.mode !== "tun" && (
        <B4FormGroup
          label={t("settings.Feature.networkInterfaces")}
          columns={1}
        >
          <Box>
            <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
              {t("settings.Feature.networkInterfacesDesc")}
            </Typography>
            <Box sx={{ display: "flex", flexWrap: "wrap", gap: 0.5 }}>
              {(config.available_ifaces ?? []).map((iface) => {
                const isSelected = (config.queue.interfaces || []).includes(
                  iface,
                );
                return (
                  <B4Badge
                    key={iface}
                    label={iface}
                    onClick={() => handleInterfaceToggle(iface)}
                    variant={isSelected ? "filled" : "outlined"}
                    color={"primary"}
                  />
                );
              })}
            </Box>
            {(config.available_ifaces ?? []).length === 0 && (
              <B4Alert severity="warning" sx={{ mt: 1 }}>
                {t("settings.Feature.noInterfacesDetected")}
              </B4Alert>
            )}
            {config.queue.interfaces?.length === 0 && (
              <B4Alert severity="info" sx={{ mt: 2 }}>
                {t("settings.Feature.listenAllInterfaces")}
              </B4Alert>
            )}
            {ifaceFilterSeesNothing && (
              <B4Alert severity="warning" sx={{ mt: 2 }}>
                {t("settings.Feature.interfaceFilterSeesNothing", {
                  selected: selectedIfaces.join(", "),
                  seen: uncovered.join(", "),
                })}
              </B4Alert>
            )}
            {ifaceFilterSeesLittle && (
              <B4Alert severity="info" sx={{ mt: 2 }}>
                {t("settings.Feature.interfaceFilterSeesLittle", {
                  percent: Math.max(1, uncoveredShare),
                  seen: uncovered.join(", "),
                })}
              </B4Alert>
            )}
          </Box>
        </B4FormGroup>
      )}
    </B4Section>
  );
};
