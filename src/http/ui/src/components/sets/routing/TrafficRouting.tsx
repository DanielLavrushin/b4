import { Box, FormHelperText, Grid, Link, Typography } from "@mui/material";
import { Link as RouterLink } from "react-router";
import {
  B4Advanced,
  B4Badge,
  B4FieldMessage,
  B4FormGroup,
  B4NumberField,
  B4Select,
  B4Switch,
  B4TextField,
  IfaceDetails,
  useIfaceLabel,
} from "@b4.elements";
import { B4SetConfig, RouterTraffic, RoutingMode } from "@models/config";
import { createDefaultSet } from "@models/defaults";
import { colors } from "@design";
import { DscpEnvironment, declaresIpTargets } from "@utils";
import { Trans, useTranslation } from "react-i18next";
import ArrowForwardIcon from "@mui/icons-material/ArrowForward";
import * as ipaddr from "ipaddr.js";
import { hasTargets } from "../facets";
import { udpPortRouting } from "./portRouting";
import { SetDscp } from "./SetDscp";

const gatewayZone = /^[^\s%/[\]]+$/;

function isGatewayAddress(value: string): boolean {
  const text = value.trim();
  if (!text) return true;
  const zoneAt = text.indexOf("%");
  if (zoneAt === -1) {
    return ipaddr.IPv4.isValidFourPartDecimal(text) || ipaddr.IPv6.isValid(text);
  }
  return (
    gatewayZone.test(text.slice(zoneAt + 1)) &&
    ipaddr.IPv6.isValid(text.slice(0, zoneAt))
  );
}

const MODE_DOCS: Record<RoutingMode, string> = {
  interface: "guides/vpn",
  proxy: "guides/proxy",
  "mtproto-ws": "telegram/websocket-bridge",
  block: "sets/blocking",
};

const DEFAULT_IP_TTL = createDefaultSet(0).routing.ip_ttl_seconds;

interface TrafficRoutingProps {
  config: B4SetConfig;
  ipv6: boolean;
  availableIfaces: string[];
  ifaceDetails?: IfaceDetails;
  tunnelIfaces?: string[];
  encapsulatedIfaces?: string[];
  dscp: DscpEnvironment;
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

export const TrafficRouting = ({
  config,
  ipv6,
  availableIfaces,
  ifaceDetails,
  tunnelIfaces = [],
  encapsulatedIfaces = [],
  dscp,
  onChange,
}: TrafficRoutingProps) => {
  const { t } = useTranslation();
  const ifaceLabel = useIfaceLabel(ifaceDetails);
  const routing = config.routing;
  const mode: RoutingMode =
    routing.mode === "proxy"
      ? "proxy"
      : routing.mode === "mtproto-ws"
        ? "mtproto-ws"
        : routing.mode === "block"
          ? "block"
          : "interface";
  const isProxy = mode === "proxy";
  const isMTProtoWS = mode === "mtproto-ws";
  const isBlock = mode === "block";
  const isInterface = mode === "interface";
  const blockAction = routing.block_action || "reject";

  const domainOnly = config.targets.domain_only ?? false;
  const hasDomains =
    (config.targets.sni_domains?.length ?? 0) > 0 ||
    (config.targets.geosite_categories?.length ?? 0) > 0;
  const showDomainOnlyRoutingWarning =
    routing.enabled && domainOnly && hasDomains;

  const hasDestination = hasTargets(config);
  const sourceDeviceCount = config.targets.source_devices?.length ?? 0;
  const selectsL7 = (config.udp.filter_l7?.length ?? 0) > 0;
  const blocksByProtocol = isBlock && selectsL7;
  const portRouting = udpPortRouting(config);
  const showNoDestinationWarning =
    routing.enabled &&
    !hasDestination &&
    !blocksByProtocol &&
    !portRouting.wanted;

  const selectedIfaceAvailable = availableIfaces.includes(
    routing.egress_interface,
  );
  const shouldShowUnavailableSelected = Boolean(
    isInterface && routing.egress_interface && !selectedIfaceAvailable,
  );
  const egressUnusable =
    shouldShowUnavailableSelected ||
    Boolean(
      isInterface &&
        ifaceDetails?.[routing.egress_interface]?.up === false,
    );

  const gatewayInvalid =
    Boolean(routing.egress_interface) &&
    !isGatewayAddress(routing.egress_gateway ?? "");

  const routerTraffic: RouterTraffic = routing.router_traffic ?? "auto";
  const egressIsTunnel = tunnelIfaces.includes(routing.egress_interface);
  const sourceScoped =
    (routing.source_interfaces?.length ?? 0) > 0 ||
    (!config.targets.source_devices_exclude && sourceDeviceCount > 0);
  const routerTrafficLoops =
    isInterface && egressIsTunnel && routerTraffic === "include" && !sourceScoped;
  const routerTrafficHeldBack =
    isInterface && egressIsTunnel && routerTraffic === "auto" && !sourceScoped;
  const egressIsEncapsulated = encapsulatedIfaces.includes(
    routing.egress_interface,
  );
  const routesAnyIP =
    !config.targets.domain_only || declaresIpTargets(config.targets);
  const strategyHandedOff =
    isInterface && egressIsEncapsulated && routesAnyIP;

  const toggleSourceIface = (iface: string) => {
    const current = routing.source_interfaces || [];
    const updated = current.includes(iface)
      ? current.filter((i) => i !== iface)
      : [...current, iface];
    onChange("routing.source_interfaces", updated);
  };

  const upstream = routing.upstream || {
    host: "",
    port: 0,
    username: "",
    password: "",
    fail_open: false,
    use_domain: true,
    udp: false,
  };

  let flowDestination: string;
  if (isProxy) {
    flowDestination =
      upstream.host && upstream.port
        ? `${upstream.host}:${upstream.port}`
        : t("sets.routing.flowNoUpstream");
  } else if (isMTProtoWS) {
    flowDestination = t("sets.routing.flowMTProtoWS");
  } else if (isBlock) {
    flowDestination = t("sets.routing.flowBlocked");
  } else {
    flowDestination = !routing.egress_interface
      ? t("sets.routing.flowNoOutput")
      : routing.egress_gateway
        ? routing.egress_ip
          ? t("sets.routing.flowIfaceWithSourceAndGateway", {
              iface: routing.egress_interface,
              ip: routing.egress_ip,
              gw: routing.egress_gateway,
            })
          : t("sets.routing.flowIfaceWithGateway", {
              iface: routing.egress_interface,
              gw: routing.egress_gateway,
            })
        : routing.egress_ip
          ? t("sets.routing.flowIfaceWithSource", {
              iface: routing.egress_interface,
              ip: routing.egress_ip,
            })
          : routing.egress_interface;
  }

  const noInterfaces = isInterface && availableIfaces.length === 0;

  const enableMessages = [
    noInterfaces && !routing.enabled && (
      <B4FieldMessage key="no-interfaces">
        {t("sets.routing.noInterfaces")}
      </B4FieldMessage>
    ),
    showNoDestinationWarning && (
      <B4FieldMessage key="no-destination">
        {sourceDeviceCount > 0
          ? t("sets.routing.noDestinationWithDevices")
          : t("sets.routing.noDestination")}
        {isProxy && ` ${t("sets.routing.noDestinationPortHint")}`}
      </B4FieldMessage>
    ),
    routing.enabled && portRouting.active && (
      <B4FieldMessage key="port-routed" severity="info">
        {t("sets.routing.portRouted", { ports: portRouting.ports })}
        {!ipv6 && ` ${t("sets.routing.portRoutedIpv6Off")}`}
      </B4FieldMessage>
    ),
    routing.enabled && portRouting.wanted && !portRouting.scoped && (
      <B4FieldMessage key="port-scope">
        {t("sets.routing.portRoutingNeedsScope", {
          ports: portRouting.ports,
        })}
      </B4FieldMessage>
    ),
    showDomainOnlyRoutingWarning && (
      <B4FieldMessage key="domain-only">
        {t("sets.routing.domainOnlyConflict")}
      </B4FieldMessage>
    ),
  ].filter(Boolean);

  const modeInfo: Record<RoutingMode, React.ReactNode> = {
    interface: t("sets.routing.modeInterfaceInfo"),
    proxy: t("sets.routing.modeProxyInfo"),
    "mtproto-ws": (
      <Trans
        i18nKey="sets.routing.modeMTProtoWSInfo"
        components={{
          a: <Link component={RouterLink} to="/settings/mtproto" />,
        }}
      />
    ),
    block: t("sets.routing.modeBlockInfo"),
  };
  const ipTtl = routing.ip_ttl_seconds || DEFAULT_IP_TTL;

  const egressMessages = [
    noInterfaces && (
      <B4FieldMessage key="no-interfaces">
        {t("sets.routing.noInterfaces")}
      </B4FieldMessage>
    ),
    egressUnusable && (
      <B4FieldMessage key="unavailable">
        {t("sets.routing.unavailableWarning", {
          iface: routing.egress_interface,
        })}
      </B4FieldMessage>
    ),
    strategyHandedOff && (
      <B4FieldMessage key="handed-off" severity="info">
        {t("sets.routing.tunnelManipulationNote", {
          iface: routing.egress_interface,
        })}
      </B4FieldMessage>
    ),
  ].filter(Boolean);

  const routerTrafficMessages = [
    sourceScoped && (
      <B4FieldMessage key="scoped" severity="info">
        {t("sets.routing.routerTrafficScoped")}
      </B4FieldMessage>
    ),
    routerTrafficLoops && (
      <B4FieldMessage key="loop">
        {t("sets.routing.routerTrafficLoopWarning", {
          iface: routing.egress_interface,
        })}
      </B4FieldMessage>
    ),
    routerTrafficHeldBack && (
      <B4FieldMessage key="held-back" severity="info">
        {t("sets.routing.routerTrafficHeldBack", {
          iface: routing.egress_interface,
        })}
      </B4FieldMessage>
    ),
  ].filter(Boolean);

  const egressOptions = [
    ...(shouldShowUnavailableSelected
      ? [
          {
            value: routing.egress_interface,
            label: t("sets.routing.interfaceUnavailableOption", {
              iface: routing.egress_interface,
            }),
          },
        ]
      : []),
    ...availableIfaces.map((iface) => ({
      value: iface,
      label: ifaceLabel(iface),
    })),
  ];

  return (
    <Grid container spacing={3}>
      <Grid size={{ xs: 12 }}>
        <B4Switch
          label={t("sets.routing.enable")}
          checked={routing.enabled}
          path="routing.enabled"
          onChange={(checked: boolean) => onChange("routing.enabled", checked)}
          description={t("sets.routing.enableDesc")}
          message={enableMessages.length > 0 ? enableMessages : undefined}
          info={t("sets.routing.enableInfo")}
          learnMore="sets/routing#traffic-routing"
          aiTopic="routing.enabled"
          disabled={noInterfaces && !routing.enabled}
        />
      </Grid>

      {routing.enabled && (
        <>
          <Grid size={{ xs: 12 }}>
            <B4Select
              label={t("sets.routing.modeLabel")}
              value={mode}
              path="routing.mode"
              onChange={(e) => onChange("routing.mode", e.target.value)}
              options={[
                {
                  value: "interface",
                  label: t("sets.routing.modeInterface"),
                  note: t("sets.routing.modeInterfaceNote"),
                },
                {
                  value: "proxy",
                  label: t("sets.routing.modeProxy"),
                  note: t("sets.routing.modeProxyNote"),
                },
                {
                  value: "mtproto-ws",
                  label: t("sets.routing.modeMTProtoWS"),
                  note: t("sets.routing.modeMTProtoWSNote"),
                },
                {
                  value: "block",
                  label: t("sets.routing.modeBlock"),
                  note: t("sets.routing.modeBlockNote"),
                },
              ]}
              helperText={t(
                isProxy
                  ? "sets.routing.modeProxyHelper"
                  : isMTProtoWS
                    ? "sets.routing.modeMTProtoWSHelper"
                    : isBlock
                      ? "sets.routing.modeBlockHelper"
                      : "sets.routing.modeInterfaceHelper",
              )}
              info={modeInfo[mode]}
              learnMore={MODE_DOCS[mode]}
              aiTopic="routing.mode"
            />
            {isBlock && (
              <Box sx={{ mt: 2 }}>
                <B4Select
                  label={t("sets.routing.blockActionLabel")}
                  value={blockAction}
                  path="routing.block_action"
                  onChange={(e) =>
                    onChange("routing.block_action", e.target.value)
                  }
                  options={[
                    {
                      value: "reject",
                      label: t("sets.routing.blockActionReject"),
                      note: t("sets.routing.blockActionRejectNote"),
                    },
                    {
                      value: "drop",
                      label: t("sets.routing.blockActionDrop"),
                      note: t("sets.routing.blockActionDropNote"),
                    },
                  ]}
                  helperText={t("sets.routing.blockActionHelper")}
                  info={t("sets.routing.blockActionInfo")}
                  learnMore="sets/blocking"
                  aiTopic="routing.block_action"
                />
              </Box>
            )}
          </Grid>

          <Grid size={{ xs: 12 }}>
            <Box
              sx={{
                p: 2,
                bgcolor: colors.background.paper,
                borderRadius: 1,
                border: `1px solid ${colors.border.default}`,
              }}
            >
              <Typography
                variant="caption"
                color="text.secondary"
                component="div"
                sx={{ mb: 1.5 }}
              >
                {t("sets.routing.flowDiagramLabel")}
              </Typography>
              <Box
                sx={{
                  display: "flex",
                  alignItems: "center",
                  gap: 1,
                  fontFamily: "monospace",
                  fontSize: "0.75rem",
                  flexWrap: "wrap",
                  justifyContent: "center",
                }}
              >
                <Box
                  sx={{
                    p: 1,
                    px: 1.5,
                    bgcolor: colors.accent.primary,
                    borderRadius: 0.5,
                    textAlign: "center",
                    whiteSpace: "nowrap",
                  }}
                >
                  {routing.source_interfaces?.length
                    ? routing.source_interfaces.join(", ")
                    : t("sets.routing.flowAnyDevice")}
                </Box>
                <ArrowForwardIcon
                  sx={{ fontSize: 16, color: "text.secondary" }}
                />
                <Box
                  sx={{
                    p: 1,
                    px: 1.5,
                    bgcolor: colors.accent.secondary,
                    borderRadius: 0.5,
                    textAlign: "center",
                    border: `1px solid ${colors.secondary}`,
                    whiteSpace: "nowrap",
                  }}
                >
                  B4
                </Box>
                <ArrowForwardIcon
                  sx={{ fontSize: 16, color: "text.secondary" }}
                />
                <Box
                  sx={{
                    p: 1,
                    px: 1.5,
                    bgcolor: colors.accent.tertiary,
                    borderRadius: 0.5,
                    textAlign: "center",
                    whiteSpace: "nowrap",
                  }}
                >
                  {flowDestination}
                </Box>
                <ArrowForwardIcon
                  sx={{ fontSize: 16, color: "text.secondary" }}
                />
                <Box
                  sx={{
                    p: 1,
                    px: 1.5,
                    bgcolor: colors.accent.primary,
                    borderRadius: 0.5,
                    textAlign: "center",
                    whiteSpace: "nowrap",
                  }}
                >
                  {t("sets.routing.flowInternet")}
                </Box>
              </Box>
            </Box>
          </Grid>

          <Grid size={{ xs: 12 }}>
            <B4FormGroup
              label={t("sets.routing.sourceInterfaces")}
              learnMore="guides/interfaces"
              aiTopic="routing.source_interfaces"
              columns={1}
            >
              <Box>
                <Box sx={{ display: "flex", flexWrap: "wrap", gap: 1 }}>
                  {(routing.source_interfaces || [])
                    .filter((iface) => !availableIfaces.includes(iface))
                    .map((iface) => (
                      <B4Badge
                        key={iface}
                        label={`${iface} (${t("sets.routing.staleIface")})`}
                        onClick={() => toggleSourceIface(iface)}
                        variant="filled"
                        color="error"
                      />
                    ))}
                  {availableIfaces.map((iface) => {
                    const selected = (routing.source_interfaces || []).includes(
                      iface,
                    );
                    return (
                      <B4Badge
                        key={iface}
                        label={ifaceLabel(iface)}
                        onClick={() => toggleSourceIface(iface)}
                        variant={selected ? "filled" : "outlined"}
                        color={selected ? "secondary" : "primary"}
                        sx={
                          ifaceDetails?.[iface]?.up === false
                            ? { opacity: 0.6 }
                            : undefined
                        }
                      />
                    );
                  })}
                </Box>
                <FormHelperText sx={{ color: "text.secondary", m: 0, mt: 0.75 }}>
                  {t("sets.routing.sourceInterfacesDesc")}
                </FormHelperText>
              </Box>
            </B4FormGroup>
          </Grid>

          {isInterface && (
            <>
              <Grid size={{ xs: 12, md: 6 }}>
                <B4Select
                  label={t("sets.routing.outputInterface")}
                  value={routing.egress_interface}
                  path="routing.egress_interface"
                  onChange={(e) =>
                    onChange("routing.egress_interface", e.target.value)
                  }
                  options={egressOptions}
                  helperText={t("sets.routing.outputInterfaceHelper")}
                  message={
                    egressMessages.length > 0 ? egressMessages : undefined
                  }
                  info={t("sets.routing.outputInterfaceInfo")}
                  learnMore="internals/routing#tunnel-hand-off"
                  aiTopic="routing.egress_interface"
                />
              </Grid>

              <Grid size={{ xs: 12, md: 6 }}>
                <B4Select
                  value={routerTraffic}
                  path="routing.router_traffic"
                  onChange={(e) =>
                    onChange("routing.router_traffic", e.target.value)
                  }
                  disabled={sourceScoped}
                  options={[
                    {
                      value: "auto",
                      label: t("sets.routing.routerTrafficAuto"),
                      note: t("sets.routing.routerTrafficAutoNote"),
                    },
                    {
                      value: "include",
                      label: t("sets.routing.routerTrafficInclude"),
                      note: t("sets.routing.routerTrafficIncludeNote"),
                    },
                    {
                      value: "exclude",
                      label: t("sets.routing.routerTrafficExclude"),
                      note: t("sets.routing.routerTrafficExcludeNote"),
                    },
                  ]}
                  helperText={t("sets.routing.routerTrafficHelper")}
                  message={
                    routerTrafficMessages.length > 0
                      ? routerTrafficMessages
                      : undefined
                  }
                  info={t("sets.routing.routerTrafficInfo")}
                  learnMore="internals/routing#router-traffic"
                  aiTopic="routing.router_traffic"
                />
              </Grid>

              <Grid size={{ xs: 12, md: 6 }}>
                <B4TextField
                  value={routing.egress_ip ?? ""}
                  path="routing.egress_ip"
                  onChange={(e) => onChange("routing.egress_ip", e.target.value)}
                  disabled={!routing.egress_interface}
                  helperText={
                    routing.egress_interface
                      ? t("sets.routing.egressIpHelper")
                      : t("sets.routing.egressIpNeedsInterface")
                  }
                  message={
                    routing.egress_ip && routing.egress_interface ? (
                      <B4FieldMessage severity="info">
                        {t("sets.routing.egressIpNote", {
                          ip: routing.egress_ip,
                          iface: routing.egress_interface,
                        })}
                      </B4FieldMessage>
                    ) : undefined
                  }
                  learnMore="internals/routing#egress-ip"
                  aiTopic="routing.egress_ip"
                  placeholder="192.168.1.51"
                  selectOnFocus
                />
              </Grid>

              <Grid size={{ xs: 12, md: 6 }}>
                <B4TextField
                  path="routing.egress_gateway"
                  value={routing.egress_gateway ?? ""}
                  onChange={(e) =>
                    onChange("routing.egress_gateway", e.target.value)
                  }
                  disabled={!routing.egress_interface}
                  error={gatewayInvalid}
                  helperText={
                    !routing.egress_interface
                      ? t("sets.routing.egressGatewayNeedsInterface")
                      : gatewayInvalid
                        ? t("sets.routing.egressGatewayInvalid")
                        : t("sets.routing.egressGatewayHelper")
                  }
                  info={t("sets.routing.egressGatewayInfo")}
                  learnMore="internals/routing#gateway"
                  aiTopic="routing.egress_gateway"
                  placeholder="192.168.1.254"
                  selectOnFocus
                />
              </Grid>

              <Grid size={{ xs: 12 }}>
                <B4Switch
                  checked={routing.kill_switch ?? false}
                  path="routing.kill_switch"
                  onChange={(checked: boolean) =>
                    onChange("routing.kill_switch", checked)
                  }
                  description={t("sets.routing.killSwitchDesc")}
                  learnMore="internals/routing#kill-switch"
                  aiTopic="routing.kill_switch"
                  disabled={!routing.egress_interface}
                />
              </Grid>
            </>
          )}

          {isProxy && (
            <>
              <Grid size={{ xs: 12, md: 8 }}>
                <B4TextField
                  value={upstream.host}
                  path="routing.upstream.host"
                  onChange={(e) =>
                    onChange("routing.upstream.host", e.target.value)
                  }
                  helperText={t("sets.routing.upstreamHostHelper")}
                  placeholder="127.0.0.1"
                  selectOnFocus
                />
              </Grid>
              <Grid size={{ xs: 12, md: 4 }}>
                <B4NumberField
                  value={upstream.port ?? 0}
                  path="routing.upstream.port"
                  onChange={(n) => onChange("routing.upstream.port", n)}
                  min={0}
                  max={65535}
                  helperText={t("sets.routing.upstreamPortHelper")}
                  placeholder="1080"
                />
              </Grid>
              <Grid size={{ xs: 12, md: 6 }}>
                <B4TextField
                  label={t("sets.routing.upstreamUser")}
                  value={upstream.username || ""}
                  path="routing.upstream.username"
                  onChange={(e) =>
                    onChange("routing.upstream.username", e.target.value)
                  }
                  helperText={t("sets.routing.upstreamAuthHelper")}
                  autoComplete="new-password"
                />
              </Grid>
              <Grid size={{ xs: 12, md: 6 }}>
                <B4TextField
                  label={t("sets.routing.upstreamPass")}
                  type="password"
                  value={upstream.password || ""}
                  path="routing.upstream.password"
                  onChange={(e) =>
                    onChange("routing.upstream.password", e.target.value)
                  }
                  helperText={t("sets.routing.upstreamAuthHelper")}
                  autoComplete="new-password"
                />
              </Grid>
              <Grid size={{ xs: 12 }}>
                <B4Switch
                  label={t("sets.routing.useDomain")}
                  checked={upstream.use_domain !== false}
                  path="routing.upstream.use_domain"
                  onChange={(checked: boolean) =>
                    onChange("routing.upstream.use_domain", checked)
                  }
                  description={t("sets.routing.useDomainDesc")}
                  info={t("sets.routing.useDomainInfo")}
                  learnMore="internals/routing#destination-names"
                  aiTopic="routing.upstream.use_domain"
                />
              </Grid>
              <Grid size={{ xs: 12 }}>
                <B4Switch
                  label={t("sets.routing.udp")}
                  checked={upstream.udp === true}
                  path="routing.upstream.udp"
                  onChange={(checked: boolean) =>
                    onChange("routing.upstream.udp", checked)
                  }
                  description={t("sets.routing.udpDesc")}
                  message={
                    upstream.udp === true ? (
                      <B4FieldMessage severity="info">
                        {t("sets.routing.udpNeedsProxy")}
                      </B4FieldMessage>
                    ) : undefined
                  }
                  info={t("sets.routing.udpInfo")}
                  learnMore="internals/routing#proxy-mode"
                  aiTopic="routing.upstream.udp"
                />
              </Grid>
              <Grid size={{ xs: 12 }}>
                <B4Switch
                  label={t("sets.routing.failOpen")}
                  checked={upstream.fail_open === true}
                  path="routing.upstream.fail_open"
                  onChange={(checked: boolean) =>
                    onChange("routing.upstream.fail_open", checked)
                  }
                  description={t("sets.routing.failOpenDesc")}
                  message={
                    upstream.fail_open ? (
                      <B4FieldMessage>
                        {t("sets.routing.failOpenWarning")}
                      </B4FieldMessage>
                    ) : undefined
                  }
                  learnMore="guides/proxy"
                  aiTopic="routing.upstream.fail_open"
                />
              </Grid>
            </>
          )}

          <Grid size={{ xs: 12 }}>
            <B4Advanced
              title={t("sets.routing.advanced")}
              paths={["routing.ip_ttl_seconds"]}
              customized={ipTtl === DEFAULT_IP_TTL ? 0 : 1}
              summary={t("sets.routing.advancedSummary", { ttl: ipTtl })}
            >
              <Grid container spacing={3}>
                <Grid size={{ xs: 12, md: 6 }}>
                  <B4NumberField
                    label={t("sets.routing.ipTtl")}
                    value={routing.ip_ttl_seconds}
                    path="routing.ip_ttl_seconds"
                    onChange={(n) => onChange("routing.ip_ttl_seconds", n)}
                    min={0}
                    helperText={t("sets.routing.ipTtlHelper")}
                    info={t("sets.routing.ipTtlInfo")}
                    learnMore="internals/routing#collecting-addresses"
                    aiTopic="routing.ip_ttl_seconds"
                    slotProps={{ inputLabel: { shrink: true } }}
                  />
                </Grid>
              </Grid>
            </B4Advanced>
          </Grid>
        </>
      )}

      <SetDscp
        config={config}
        env={dscp}
        availableIfaces={availableIfaces}
        ifaceDetails={ifaceDetails}
        encapsulatedIfaces={encapsulatedIfaces}
        onChange={onChange}
      />
    </Grid>
  );
};
