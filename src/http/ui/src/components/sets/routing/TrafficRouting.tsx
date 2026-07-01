import { Box, Grid, MenuItem, Typography } from "@mui/material";
import {
  B4Alert,
  B4Badge,
  B4Hint,
  B4NumberField,
  B4Switch,
  B4TextField,
} from "@b4.elements";
import { B4SetConfig, RoutingMode } from "@models/config";
import { colors } from "@design";
import { useTranslation } from "react-i18next";
import ArrowForwardIcon from "@mui/icons-material/ArrowForward";

interface TrafficRoutingProps {
  config: B4SetConfig;
  availableIfaces: string[];
  onChange: (
    field: string,
    value: string | number | boolean | string[] | number[] | null | undefined,
  ) => void;
}

export const TrafficRouting = ({
  config,
  availableIfaces,
  onChange,
}: TrafficRoutingProps) => {
  const { t } = useTranslation();
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

  const selectedIfaceAvailable = availableIfaces.includes(
    routing.egress_interface,
  );
  const shouldShowUnavailableSelected = Boolean(
    isInterface && routing.egress_interface && !selectedIfaceAvailable,
  );

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
    flowDestination =
      routing.egress_interface || t("sets.routing.flowNoOutput");
  }

  return (
    <Grid container spacing={3}>
      <Grid size={{ lg: 12 }}>
        <B4Switch
          label={t("sets.routing.enable")}
          checked={routing.enabled}
          onChange={(checked: boolean) => onChange("routing.enabled", checked)}
          description={t("sets.routing.enableDesc")}
          disabled={
            isInterface && availableIfaces.length === 0 && !routing.enabled
          }
        />
      </Grid>

      {routing.enabled && (
        <>
          {showDomainOnlyRoutingWarning && (
            <Grid size={{ xs: 12 }}>
              <B4Alert severity="warning">
                {t("sets.routing.domainOnlyConflict")}
              </B4Alert>
            </Grid>
          )}
          <Grid size={{ xs: 12 }}>
            <B4TextField
              label={t("sets.routing.modeLabel")}
              select
              value={mode}
              onChange={(e) => onChange("routing.mode", e.target.value)}
              helperText={t("sets.routing.modeHelper")}
            >
              <MenuItem value="interface">
                {t("sets.routing.modeInterface")}
              </MenuItem>
              <MenuItem value="proxy">{t("sets.routing.modeProxy")}</MenuItem>
              <MenuItem value="mtproto-ws">
                {t("sets.routing.modeMTProtoWS")}
              </MenuItem>
              <MenuItem value="block">{t("sets.routing.modeBlock")}</MenuItem>
            </B4TextField>
            {isMTProtoWS && (
              <B4Alert severity="info" sx={{ mt: 2 }}>
                {t("sets.routing.mtprotoWsNote")}
              </B4Alert>
            )}
            {isBlock && (
              <B4TextField
                label={t("sets.routing.blockActionLabel")}
                select
                value={blockAction}
                onChange={(e) =>
                  onChange("routing.block_action", e.target.value)
                }
                helperText={t("sets.routing.blockActionHelper")}
                sx={{ mt: 2 }}
              >
                <MenuItem value="reject">
                  {t("sets.routing.blockActionReject")}
                </MenuItem>
                <MenuItem value="drop">
                  {t("sets.routing.blockActionDrop")}
                </MenuItem>
              </B4TextField>
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
              <Typography
                variant="caption"
                color="text.secondary"
                sx={{ mt: 1.5, display: "block", textAlign: "center" }}
              >
                {isProxy
                  ? t("sets.routing.flowProxyCaption")
                  : isMTProtoWS
                    ? t("sets.routing.flowMTProtoWSCaption")
                    : isBlock
                      ? t("sets.routing.flowBlockCaption")
                      : t("sets.routing.flowCaption")}
              </Typography>
            </Box>
          </Grid>

          <B4Hint>
            {isProxy
              ? t("sets.routing.howItWorksProxy")
              : isMTProtoWS
                ? t("sets.routing.howItWorksMTProtoWS")
                : isBlock
                  ? t("sets.routing.howItWorksBlock")
                  : t("sets.routing.howItWorks")}
          </B4Hint>

          <Grid size={{ xs: 12 }}>
            <Typography variant="subtitle2" sx={{ mb: 1 }}>
              {t("sets.routing.sourceInterfaces")}
            </Typography>
            <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
              {t("sets.routing.sourceInterfacesDesc")}
            </Typography>

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
                    label={iface}
                    onClick={() => toggleSourceIface(iface)}
                    variant={selected ? "filled" : "outlined"}
                    color={selected ? "secondary" : "primary"}
                  />
                );
              })}
            </Box>

            {availableIfaces.length === 0 && isInterface && (
              <B4Alert severity="warning" sx={{ mt: 2 }}>
                {t("sets.routing.noInterfaces")}
              </B4Alert>
            )}

            {shouldShowUnavailableSelected && (
              <B4Alert severity="warning" sx={{ mt: 2 }}>
                {t("sets.routing.unavailableWarning", {
                  iface: routing.egress_interface,
                })}
              </B4Alert>
            )}

            <B4Hint sx={{ mt: 2 }}>
              {isProxy
                ? t("sets.routing.infoProxy")
                : isMTProtoWS
                  ? t("sets.routing.infoMTProtoWS")
                  : isBlock
                    ? t("sets.routing.infoBlock")
                    : t("sets.routing.info")}
            </B4Hint>
          </Grid>

          {isInterface && (
            <Grid size={{ xs: 12, md: 6 }}>
              <B4TextField
                label={t("sets.routing.outputInterface")}
                select
                value={routing.egress_interface}
                onChange={(e) =>
                  onChange("routing.egress_interface", e.target.value)
                }
                helperText={
                  shouldShowUnavailableSelected
                    ? t("sets.routing.interfaceUnavailable")
                    : t("sets.routing.outputInterfaceHelper")
                }
              >
                {shouldShowUnavailableSelected && (
                  <MenuItem value={routing.egress_interface}>
                    {t("sets.routing.interfaceUnavailableOption", {
                      iface: routing.egress_interface,
                    })}
                  </MenuItem>
                )}
                {availableIfaces.map((iface) => (
                  <MenuItem key={iface} value={iface}>
                    {iface}
                  </MenuItem>
                ))}
              </B4TextField>
            </Grid>
          )}

          {isProxy && (
            <>
              <Grid size={{ xs: 12, md: 8 }}>
                <B4TextField
                  label={t("sets.routing.upstreamHost")}
                  value={upstream.host}
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
                  label={t("sets.routing.upstreamPort")}
                  value={upstream.port ?? 0}
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
                  onChange={(checked: boolean) =>
                    onChange("routing.upstream.use_domain", checked)
                  }
                  description={t("sets.routing.useDomainDesc")}
                />
              </Grid>
              <Grid size={{ xs: 12 }}>
                <B4Switch
                  label={t("sets.routing.udp")}
                  checked={upstream.udp === true}
                  onChange={(checked: boolean) =>
                    onChange("routing.upstream.udp", checked)
                  }
                  description={t("sets.routing.udpDesc")}
                />
              </Grid>
              <Grid size={{ xs: 12 }}>
                <B4Switch
                  label={t("sets.routing.failOpen")}
                  checked={upstream.fail_open === true}
                  onChange={(checked: boolean) =>
                    onChange("routing.upstream.fail_open", checked)
                  }
                  description={t("sets.routing.failOpenDesc")}
                />
                {upstream.fail_open && (
                  <B4Alert severity="warning" sx={{ mt: 1 }}>
                    {t("sets.routing.failOpenWarning")}
                  </B4Alert>
                )}
              </Grid>
              <Grid size={{ xs: 12 }}>
                <B4Hint>{t("sets.routing.proxyManipulationNote")}</B4Hint>
              </Grid>
            </>
          )}

          <Grid size={{ xs: 12, md: 6 }}>
            <B4NumberField
              label={t("sets.routing.ipTtl")}
              value={routing.ip_ttl_seconds}
              onChange={(n) => onChange("routing.ip_ttl_seconds", n)}
              min={0}
              helperText={t("sets.routing.ipTtlHelper")}
              slotProps={{ inputLabel: { shrink: true } }}
            />
            <Typography
              variant="caption"
              color="text.secondary"
              sx={{ mt: 1, display: "block" }}
            >
              {t("sets.routing.ipTtlDesc")}
            </Typography>
          </Grid>
        </>
      )}
    </Grid>
  );
};
