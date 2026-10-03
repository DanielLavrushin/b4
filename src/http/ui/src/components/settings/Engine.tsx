import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { NetworkIcon, ToggleOnIcon } from "@b4.icons";
import { B4Config } from "@models/config";
import { systemApi } from "@api/settings";
import {
  B4FormGroup,
  B4Section,
  B4Switch,
  B4Select,
  B4Alert,
  B4Badge,
  B4TextField,
} from "@b4.elements";
import { Box, Typography } from "@mui/material";
import { EngineFailure, SettingsPropHandlerType } from "@models/settings";

interface EngineCardProps {
  config: B4Config;
  onChange: (field: string, value: SettingsPropHandlerType) => void;
}

const IPV6_BYPASS_DISMISS_KEY = "b4_ipv6_bypass_dismissed";
const ENGINE_FAILURE_RECHECK_MS = 15000;

export const useEngineStatus = () => {
  const [ipv6BypassesSets, setIpv6BypassesSets] = useState(false);
  const [engineFailure, setEngineFailure] = useState<EngineFailure | null>(
    null,
  );

  useEffect(() => {
    systemApi
      .info()
      .then((info) => {
        setIpv6BypassesSets(!!info?.ipv6_bypasses_sets);
        setEngineFailure(info?.engine_failure ?? null);
      })
      .catch(() => setIpv6BypassesSets(false));
  }, []);

  const engineFailed = engineFailure !== null;
  useEffect(() => {
    if (!engineFailed) return;
    const timer = setInterval(() => {
      systemApi
        .info()
        .then((info) => setEngineFailure(info?.engine_failure ?? null))
        .catch(() => {});
    }, ENGINE_FAILURE_RECHECK_MS);
    return () => clearInterval(timer);
  }, [engineFailed]);

  return { ipv6BypassesSets, engineFailure };
};

export const PacketEngineSettings = ({
  config,
  onChange,
  engineFailure,
}: EngineCardProps & { engineFailure: EngineFailure | null }) => {
  const { t } = useTranslation();

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

  const handleInterfaceToggle = (iface: string) => {
    const current = config.queue.interfaces || [];
    const updated = current.includes(iface)
      ? current.filter((i) => i !== iface)
      : [...current, iface];
    onChange("queue.interfaces", updated);
  };

  const tunOutInterface = config.queue.tun?.out_interface;
  const tunFollowsDefault = !tunOutInterface || tunOutInterface === "auto";

  const tunSettings = (
    <B4FormGroup label={t("settings.Feature.tunSettings")} columns={2}>
      <B4Select
        label={t("settings.Feature.tunOutInterface")}
        value={tunFollowsDefault ? "" : tunOutInterface ?? ""}
        onChange={(e) => onChange("queue.tun.out_interface", e.target.value)}
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
            : "settings.Feature.tunOutGatewayHelp",
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
    </B4FormGroup>
  );

  const captureInterfaces = (
    <B4FormGroup label={t("settings.Feature.networkInterfaces")} columns={1}>
      <Box>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
          {t("settings.Feature.networkInterfacesDesc")}
        </Typography>
        <Box sx={{ display: "flex", flexWrap: "wrap", gap: 0.5 }}>
          {(config.available_ifaces ?? []).map((iface) => {
            const isSelected = (config.queue.interfaces || []).includes(iface);
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
  );

  return (
    <B4Section title={t("settings.Feature.engineMode")} icon={<NetworkIcon />}>
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
      {engineFailure && (
        <B4Alert severity="error" noWrapper>
          {t("settings.Feature.engineFailed", {
            engine: engineFailure.mode === "tun" ? "TUN" : "NFQUEUE",
            error: engineFailure.error,
          })}
        </B4Alert>
      )}
      {config.queue.mode === "tun" ? tunSettings : captureInterfaces}
    </B4Section>
  );
};

export const IpVersionSettings = ({
  config,
  onChange,
  ipv6BypassesSets,
}: EngineCardProps & { ipv6BypassesSets: boolean }) => {
  const { t } = useTranslation();
  const [ipv6BypassDismissed, setIpv6BypassDismissed] = useState(() => {
    try {
      return localStorage.getItem(IPV6_BYPASS_DISMISS_KEY) === "true";
    } catch {
      return false;
    }
  });

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
  const keepIpv6Answers = config.system.dns?.keep_ipv6_answers ?? false;

  return (
    <B4Section
      title={t("settings.Feature.protoFeatures")}
      icon={<ToggleOnIcon />}
    >
      <B4Switch
        label={t("settings.Feature.enableIPv4")}
        checked={config.queue.ipv4}
        onChange={(checked: boolean) => onChange("queue.ipv4", checked)}
      />
      <B4Switch
        label={t("settings.Feature.enableIPv6")}
        checked={config.queue.ipv6}
        onChange={(checked: boolean) => onChange("queue.ipv6", checked)}
        description={t("settings.Feature.enableIPv6Desc")}
      />
      <B4Switch
        label={t("settings.Dns.ipv4Fallback")}
        checked={!keepIpv6Answers}
        onChange={(checked: boolean) =>
          onChange("system.dns.keep_ipv6_answers", !checked)
        }
        disabled={config.queue.ipv6}
        description={t(
          config.queue.ipv6
            ? "settings.Dns.ipv4FallbackIdle"
            : "settings.Dns.ipv4FallbackDesc",
        )}
      />
      {showIpv6Bypass && (
        <B4Alert severity="warning" noWrapper onClose={dismissIpv6Bypass}>
          {t("settings.Feature.ipv6BypassWarning")}
        </B4Alert>
      )}
    </B4Section>
  );
};
