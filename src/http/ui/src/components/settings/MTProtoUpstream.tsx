import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  Box,
  Button,
  Chip,
  CircularProgress,
  Grid,
  IconButton,
  InputAdornment,
  Stack,
  Tooltip,
} from "@mui/material";
import CheckIcon from "@mui/icons-material/Check";
import CloseIcon from "@mui/icons-material/Close";
import OpenInNewIcon from "@mui/icons-material/OpenInNew";
import HelpOutlineIcon from "@mui/icons-material/HelpOutline";
import NetworkPingIcon from "@mui/icons-material/NetworkPing";
import { RoutingIcon } from "@b4.icons";
import {
  B4Accordion,
  B4Alert,
  B4IntegrationCard,
  B4NumberField,
  B4Select,
  B4Switch,
  B4TextField,
} from "@b4.elements";
import { B4Config } from "@models/config";
import { SettingsPropHandlerType } from "@models/settings";
import { MTProtoRelayHelpDialog } from "./MTProtoRelayHelpDialog";

type WsProbeResult = {
  transport: string;
  ok: boolean;
  stage?: string;
  latency_ms?: number;
  hold_ms?: number;
  error?: string;
};

type RefreshResult =
  | {
      ok: true;
      count: number;
      dcs: Record<string, string>;
      direct?: Record<string, string>;
      direct_v6?: Record<string, string>;
    }
  | {
      ok: false;
      error: string;
      direct?: Record<string, string>;
      direct_v6?: Record<string, string>;
    };

const upstreamDescSuffix = (mode: string) => {
  if (mode === "tcp") return "Tcp";
  if (mode === "ws") return "Ws";
  return "Auto";
};

const normalizeHost = (raw: string) =>
  raw
    .trim()
    .replace(/^[a-z][a-z0-9+.-]*:\/\//i, "")
    .replace(/[/?#].*$/, "")
    .trim();

const normalizeWorkerDomains = (raw: string) =>
  raw.split(",").map(normalizeHost).filter(Boolean).join(", ");

interface MTProtoUpstreamCardProps {
  config: B4Config;
  onChange: (field: string, value: SettingsPropHandlerType) => void;
}

export const MTProtoUpstreamCard = ({
  config,
  onChange,
}: MTProtoUpstreamCardProps) => {
  const { t } = useTranslation();
  const [relayHelpOpen, setRelayHelpOpen] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [refreshResult, setRefreshResult] = useState<RefreshResult | null>(
    null,
  );
  const [wsTesting, setWsTesting] = useState<null | "configured" | "direct">(
    null,
  );
  const [wsResults, setWsResults] = useState<WsProbeResult[] | null>(null);
  const [wsTestError, setWsTestError] = useState<string | null>(null);

  const mtproto = config.system.mtproto;
  const mode = mtproto?.upstream_mode || "auto";
  const dcRelay = mtproto?.dc_relay || "";
  const showDcRelay = !!dcRelay || mode === "tcp" || mode === "auto";

  const relayInfo = useMemo(() => {
    if (!dcRelay) return null;
    const m = /^(\[[^\]]+\]|[^:]+):(\d+)$/.exec(dcRelay);
    if (!m) return null;
    const basePort = Number(m[2]);
    if (!basePort || basePort < 1 || basePort > 65535) return null;
    return {
      host: m[1].replaceAll(/^\[|\]$/g, ""),
      hostIsV6: m[1].startsWith("["),
      basePort,
    };
  }, [dcRelay]);

  const handleRefreshDCs = async () => {
    setRefreshing(true);
    setRefreshResult(null);
    try {
      const res = await fetch("/api/mtproto/refresh-dcs", { method: "POST" });
      const data = (await res.json()) as {
        success: boolean;
        count?: number;
        dcs?: Record<string, string>;
        direct?: Record<string, string>;
        direct_v6?: Record<string, string>;
        error?: string;
      };
      if (data.success && typeof data.count === "number" && data.dcs) {
        setRefreshResult({
          ok: true,
          count: data.count,
          dcs: data.dcs,
          direct: data.direct,
          direct_v6: data.direct_v6,
        });
      } else {
        setRefreshResult({
          ok: false,
          error: data.error || "unknown error",
          direct: data.direct,
          direct_v6: data.direct_v6,
        });
      }
    } catch (e) {
      setRefreshResult({ ok: false, error: String(e) });
    } finally {
      setRefreshing(false);
    }
  };

  const openRelayHelp = () => {
    setRelayHelpOpen(true);
    if (!refreshResult?.ok && !refreshing) {
      void handleRefreshDCs();
    }
  };

  const runProbe = async (
    which: "configured" | "direct",
    overrides: Record<string, unknown>,
  ) => {
    setWsTesting(which);
    setWsResults(null);
    setWsTestError(null);
    try {
      const res = await fetch("/api/mtproto/test-ws", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          upstream_mode: mtproto?.upstream_mode || "auto",
          ws_custom_domain: mtproto?.ws_custom_domain || "",
          ws_endpoint_host: mtproto?.ws_endpoint_host || "",
          cfworker_domain: mtproto?.cfworker_domain || "",
          cfproxy_enabled: mtproto?.cfproxy_enabled ?? true,
          dc: 2,
          ...overrides,
        }),
      });
      const data = (await res.json()) as {
        success: boolean;
        results?: WsProbeResult[];
        error?: string;
      };
      if (data.success && data.results) {
        setWsResults(data.results);
      } else {
        setWsTestError(data.error || "unknown error");
      }
    } catch (e) {
      setWsTestError(String(e));
    } finally {
      setWsTesting(null);
    }
  };

  const handleTestWS = () => runProbe("configured", {});
  const handleTestDirectTCP = () =>
    runProbe("direct", { upstream_mode: "tcp", dc_relay: "" });

  return (
    <B4IntegrationCard
      icon={<RoutingIcon />}
      title={t("settings.MTProto.upstreamTitle")}
      description={t("settings.MTProto.upstreamDesc")}
    >
      <Grid container spacing={2}>
        <Grid size={{ xs: 12, md: showDcRelay ? 6 : 12 }}>
          <B4Select
            label={t("settings.MTProto.upstreamMode")}
            value={mode}
            onChange={(e) =>
              onChange("system.mtproto.upstream_mode", String(e.target.value))
            }
            options={[
              { value: "tcp", label: t("settings.MTProto.upstreamTcp") },
              { value: "auto", label: t("settings.MTProto.upstreamAuto") },
              { value: "ws", label: t("settings.MTProto.upstreamWs") },
            ]}
            helperText={
              mode === "auto" && dcRelay
                ? t("settings.MTProto.upstreamAutoRelayDesc")
                : t(`settings.MTProto.upstream${upstreamDescSuffix(mode)}Desc`)
            }
          />
        </Grid>
        {showDcRelay && (
          <Grid size={{ xs: 12, md: 6 }}>
            <B4TextField
              label={t("settings.MTProto.dcRelay")}
              value={dcRelay}
              onChange={(e) =>
                onChange("system.mtproto.dc_relay", e.target.value)
              }
              placeholder="vps-ip:7007"
              helperText={t("settings.MTProto.dcRelayHelp")}
              selectOnFocus
              slotProps={{
                input: {
                  endAdornment: (
                    <InputAdornment position="end" sx={{ mr: -0.5 }}>
                      <Tooltip title={t("settings.MTProto.dcRelayHelpButton")}>
                        <span style={{ display: "inline-flex" }}>
                          <IconButton
                            size="small"
                            onClick={openRelayHelp}
                            sx={{ px: 0 }}
                          >
                            <HelpOutlineIcon fontSize="small" />
                          </IconButton>
                        </span>
                      </Tooltip>
                    </InputAdornment>
                  ),
                },
              }}
            />
          </Grid>
        )}
        <Grid size={{ xs: 12 }}>
          <B4TextField
            label={t("settings.MTProto.cfWorkerDomain")}
            value={mtproto?.cfworker_domain || ""}
            onChange={(e) =>
              onChange("system.mtproto.cfworker_domain", e.target.value)
            }
            onBlur={(e) => {
              const cleaned = normalizeWorkerDomains(e.target.value);
              if (cleaned !== e.target.value) {
                onChange("system.mtproto.cfworker_domain", cleaned);
              }
            }}
            placeholder="your-worker.workers.dev"
            helperText={t("settings.MTProto.cfWorkerDomainHelp")}
            selectOnFocus
            slotProps={{
              input: {
                endAdornment: (
                  <InputAdornment position="end" sx={{ mr: -0.5 }}>
                    <Tooltip title={t("settings.MTProto.cfWorkerDomainGuide")}>
                      <span style={{ display: "inline-flex" }}>
                        <IconButton
                          size="small"
                          component="a"
                          href="https://github.com/Flowseal/tg-ws-proxy/blob/main/docs/CfWorker.md"
                          target="_blank"
                          rel="noreferrer"
                          sx={{ px: 0 }}
                        >
                          <OpenInNewIcon fontSize="small" />
                        </IconButton>
                      </span>
                    </Tooltip>
                  </InputAdornment>
                ),
              },
            }}
          />
        </Grid>
        <Grid size={{ xs: 12, md: 6 }}>
          <B4TextField
            label={t("settings.MTProto.wsCustomDomain")}
            value={mtproto?.ws_custom_domain || ""}
            onChange={(e) =>
              onChange("system.mtproto.ws_custom_domain", e.target.value)
            }
            onBlur={(e) => {
              const cleaned = normalizeHost(e.target.value);
              if (cleaned !== e.target.value) {
                onChange("system.mtproto.ws_custom_domain", cleaned);
              }
            }}
            placeholder="your-domain.com"
            helperText={t("settings.MTProto.wsCustomDomainHelp")}
            selectOnFocus
          />
        </Grid>
        <Grid size={{ xs: 12, md: 6 }}>
          <B4TextField
            label={t("settings.MTProto.wsEndpointHost")}
            value={mtproto?.ws_endpoint_host || ""}
            onChange={(e) =>
              onChange("system.mtproto.ws_endpoint_host", e.target.value)
            }
            onBlur={(e) => {
              const cleaned = normalizeHost(e.target.value);
              if (cleaned !== e.target.value) {
                onChange("system.mtproto.ws_endpoint_host", cleaned);
              }
            }}
            placeholder="149.154.167.220"
            helperText={t("settings.MTProto.wsEndpointHostHelp")}
            selectOnFocus
          />
        </Grid>
      </Grid>

      <B4Accordion title={t("settings.MTProto.upstreamFallbacks")}>
        <Stack spacing={2}>
          <Box sx={{ display: "flex", flexDirection: "column", gap: 1.5 }}>
            <B4Switch
              label={t("settings.MTProto.cfProxyEnabled")}
              checked={mtproto?.cfproxy_enabled ?? true}
              onChange={(checked: boolean) =>
                onChange("system.mtproto.cfproxy_enabled", checked)
              }
              description={t("settings.MTProto.cfProxyEnabledHelp")}
            />
            {mtproto?.cfproxy_enabled !== false && (
              <B4TextField
                label={t("settings.MTProto.cfProxyURL")}
                value={mtproto?.cfproxy_url || ""}
                onChange={(e) =>
                  onChange("system.mtproto.cfproxy_url", e.target.value)
                }
                placeholder="https://raw.githubusercontent.com/Flowseal/tg-ws-proxy/main/.github/cfproxy-domains.txt"
                helperText={t("settings.MTProto.cfProxyURLHelp")}
                selectOnFocus
              />
            )}
          </Box>
          <Box sx={{ display: "flex", flexDirection: "column", gap: 1.5 }}>
            <B4Switch
              label={t("settings.MTProto.dcFallbackEnabled")}
              checked={mtproto?.dc_fallback_enabled ?? true}
              onChange={(checked: boolean) =>
                onChange("system.mtproto.dc_fallback_enabled", checked)
              }
              description={t("settings.MTProto.dcFallbackEnabledHelp")}
            />
            {mtproto?.dc_fallback_enabled !== false && (
              <B4TextField
                label={t("settings.MTProto.dcFallbackURL")}
                value={mtproto?.dc_fallback_url || ""}
                onChange={(e) =>
                  onChange("system.mtproto.dc_fallback_url", e.target.value)
                }
                placeholder="https://proxy.b4core.app/telegram/getProxyConfig"
                helperText={t("settings.MTProto.dcFallbackURLHelp")}
                selectOnFocus
              />
            )}
          </Box>
          <B4NumberField
            label={t("settings.MTProto.bridgeWait")}
            value={mtproto?.bridge_wait_sec || 180}
            onChange={(n) => onChange("system.mtproto.bridge_wait_sec", n)}
            min={-1}
            max={86400}
            helperText={t("settings.MTProto.bridgeWaitHelp")}
          />
        </Stack>
      </B4Accordion>

      <Box sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
        <Stack direction="row" spacing={1}>
          <Button
            variant="outlined"
            size="small"
            startIcon={
              wsTesting === "configured" ? (
                <CircularProgress size={14} />
              ) : (
                <NetworkPingIcon fontSize="small" />
              )
            }
            onClick={() => void handleTestWS()}
            disabled={wsTesting !== null}
          >
            {wsTesting === "configured"
              ? t("settings.MTProto.testWsRunning")
              : t("settings.MTProto.testWs")}
          </Button>
          <Tooltip title={t("settings.MTProto.testDirectTcpHelp")}>
            <span>
              <Button
                variant="outlined"
                size="small"
                startIcon={
                  wsTesting === "direct" ? (
                    <CircularProgress size={14} />
                  ) : undefined
                }
                onClick={() => void handleTestDirectTCP()}
                disabled={wsTesting !== null}
              >
                {wsTesting === "direct"
                  ? t("settings.MTProto.testWsRunning")
                  : t("settings.MTProto.testDirectTcp")}
              </Button>
            </span>
          </Tooltip>
        </Stack>
        {wsTestError && <B4Alert severity="error">{wsTestError}</B4Alert>}
        {wsResults && (
          <Stack spacing={0.5}>
            {wsResults.map((r) => {
              let label: string;
              if (r.ok) {
                const parts = [`${r.latency_ms} ms`];
                if (r.hold_ms != null) {
                  parts.push(
                    t("settings.MTProto.testHeldMs", { ms: r.hold_ms }),
                  );
                }
                label = `${r.transport} · ${parts.join(", ")}`;
              } else {
                const stageLabel = r.stage
                  ? t(`settings.MTProto.testStage_${r.stage}`, {
                      defaultValue: r.stage,
                    })
                  : "";
                label = stageLabel
                  ? `${r.transport} · [${stageLabel}] ${r.error}`
                  : `${r.transport} · ${r.error}`;
              }
              return (
                <Chip
                  key={r.transport}
                  size="small"
                  icon={
                    r.ok ? (
                      <CheckIcon fontSize="small" />
                    ) : (
                      <CloseIcon fontSize="small" />
                    )
                  }
                  color={r.ok ? "success" : "default"}
                  variant={r.ok ? "filled" : "outlined"}
                  label={label}
                  sx={{
                    justifyContent: "flex-start",
                    maxWidth: "100%",
                    "& .MuiChip-label": {
                      overflow: "hidden",
                      textOverflow: "ellipsis",
                    },
                  }}
                />
              );
            })}
          </Stack>
        )}
      </Box>

      <MTProtoRelayHelpDialog
        open={relayHelpOpen}
        onClose={() => setRelayHelpOpen(false)}
        relayInfo={relayInfo}
        refreshResult={refreshResult}
        refreshing={refreshing}
        onRefresh={() => void handleRefreshDCs()}
      />
    </B4IntegrationCard>
  );
};
