import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  Button,
  Box,
  CircularProgress,
  IconButton,
  InputAdornment,
  Link,
  Tooltip,
  Typography,
  Chip,
  Stack,
} from "@mui/material";
import ContentCopyIcon from "@mui/icons-material/ContentCopy";
import CheckIcon from "@mui/icons-material/Check";
import CloseIcon from "@mui/icons-material/Close";
import IosShareIcon from "@mui/icons-material/IosShare";
import OpenInNewIcon from "@mui/icons-material/OpenInNew";
import HelpOutlineIcon from "@mui/icons-material/HelpOutline";
import NetworkPingIcon from "@mui/icons-material/NetworkPing";
import { MTProtoRelayHelpDialog } from "./MTProtoRelayHelpDialog";
import { MTProtoSecrets } from "./MTProtoSecrets";
import { QRCodeSVG } from "qrcode.react";
import { ConnectionIcon } from "@b4.icons";
import {
  B4FormGroup,
  B4NumberField,
  B4Section,
  B4Select,
  B4Switch,
  B4TextField,
  B4Alert,
  B4Dialog,
} from "@b4.elements";
import { copyText } from "@utils";
import { B4Config } from "@models/config";
import { SettingsPropHandlerType } from "@models/settings";

type WsProbeResult = {
  transport: string;
  ok: boolean;
  stage?: string;
  latency_ms?: number;
  hold_ms?: number;
  error?: string;
};

const upstreamDescSuffix = (mode: string) => {
  if (mode === "tcp") return "Tcp";
  if (mode === "ws") return "Ws";
  return "Auto";
};

const normalizeWorkerDomains = (raw: string) =>
  raw
    .split(",")
    .map((d) =>
      d
        .trim()
        .replace(/^[a-z][a-z0-9+.-]*:\/\//i, "")
        .replace(/[/?#].*$/, "")
        .trim(),
    )
    .filter(Boolean)
    .join(", ");

interface MTProtoSettingsProps {
  config: B4Config;
  onChange: (field: string, value: SettingsPropHandlerType) => void;
}

export const MTProtoSettings = ({ config, onChange }: MTProtoSettingsProps) => {
  const { t } = useTranslation();
  const [refreshing, setRefreshing] = useState(false);
  const [refreshResult, setRefreshResult] = useState<
    | { ok: true; count: number; dcs: Record<string, string> }
    | { ok: false; error: string }
    | null
  >(null);
  const [shareOpen, setShareOpen] = useState(false);
  const [shareHost, setShareHost] = useState("");
  const [shareSecret, setShareSecret] = useState("");
  const [copied, setCopied] = useState(false);
  const [relayHelpOpen, setRelayHelpOpen] = useState(false);
  const [wsTesting, setWsTesting] = useState<null | "configured" | "direct">(
    null,
  );
  const [wsResults, setWsResults] = useState<WsProbeResult[] | null>(null);
  const [wsTestError, setWsTestError] = useState<string | null>(null);

  const port = config.system.mtproto?.port ?? 3128;
  const dcRelay = config.system.mtproto?.dc_relay || "";

  const relayInfo = useMemo(() => {
    if (!dcRelay) return null;
    const m = /^(\[[^\]]+\]|[^:]+):(\d+)$/.exec(dcRelay);
    if (!m) return null;
    const basePort = Number(m[2]);
    if (!basePort || basePort < 1 || basePort > 65535) return null;
    return { host: m[1].replaceAll(/^\[|\]$/g, ""), basePort };
  }, [dcRelay]);

  const shareLink = useMemo(() => {
    const host = (shareHost || "").trim();
    if (!host || !shareSecret) return "";
    return `tg://proxy?server=${encodeURIComponent(host)}&port=${port}&secret=${encodeURIComponent(shareSecret)}`;
  }, [shareHost, port, shareSecret]);
  const canShare =
    typeof navigator !== "undefined" && typeof navigator.share === "function";

  const openShare = (secretValue: string) => {
    const bind = config.system.mtproto?.bind_address || "";
    const isAnyAddr = !bind || bind === "0.0.0.0" || bind === "::";
    setShareHost(isAnyAddr ? globalThis.location.hostname : bind);
    setShareSecret(secretValue);
    setCopied(false);
    setShareOpen(true);
  };

  const handleCopy = async () => {
    if (!shareLink) return;
    if (await copyText(shareLink)) {
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    }
  };

  const handleNativeShare = async () => {
    if (!shareLink || !canShare) return;
    try {
      await navigator.share({
        title: t("settings.MTProto.title"),
        url: shareLink,
      });
    } catch {
      /* user cancelled */
    }
  };

  const openRelayHelp = () => {
    setRelayHelpOpen(true);
    if (!refreshResult?.ok && !refreshing) {
      void handleRefreshDCs();
    }
  };

  const handleRefreshDCs = async () => {
    setRefreshing(true);
    setRefreshResult(null);
    try {
      const res = await fetch("/api/mtproto/refresh-dcs", { method: "POST" });
      const data = (await res.json()) as {
        success: boolean;
        count?: number;
        dcs?: Record<string, string>;
        error?: string;
      };
      if (data.success && typeof data.count === "number" && data.dcs) {
        setRefreshResult({ ok: true, count: data.count, dcs: data.dcs });
      } else {
        setRefreshResult({ ok: false, error: data.error || "unknown error" });
      }
    } catch (e) {
      setRefreshResult({ ok: false, error: String(e) });
    } finally {
      setRefreshing(false);
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
          upstream_mode: config.system.mtproto?.upstream_mode || "auto",
          ws_custom_domain: config.system.mtproto?.ws_custom_domain || "",
          ws_endpoint_host: config.system.mtproto?.ws_endpoint_host || "",
          cfworker_domain: config.system.mtproto?.cfworker_domain || "",
          cfproxy_enabled: config.system.mtproto?.cfproxy_enabled ?? true,
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
    <B4Section
      title={t("settings.MTProto.title")}
      description={t("settings.MTProto.description")}
      icon={<ConnectionIcon />}
    >
      <B4FormGroup
        label={t("settings.MTProto.settings")}
        description={t("settings.MTProto.serverDesc")}
        columns={2}
      >
        <B4Switch
          label={t("settings.MTProto.enable")}
          checked={config.system.mtproto?.enabled ?? false}
          onChange={(checked: boolean) =>
            onChange("system.mtproto.enabled", checked)
          }
          description={t("settings.MTProto.enableDesc")}
        />
        <B4TextField
          label={t("settings.MTProto.bindAddress")}
          value={config.system.mtproto?.bind_address || "0.0.0.0"}
          onChange={(e) =>
            onChange("system.mtproto.bind_address", e.target.value)
          }
          placeholder={t("settings.MTProto.bindAddressPlaceholder")}
          disabled={!config.system.mtproto?.enabled}
          helperText={t("settings.MTProto.bindAddressHelp")}
          selectOnFocus
        />
        <B4NumberField
          label={t("settings.MTProto.port")}
          value={config.system.mtproto?.port ?? 3128}
          onChange={(n) => onChange("system.mtproto.port", n)}
          min={1}
          max={65535}
          disabled={!config.system.mtproto?.enabled}
          helperText={t("settings.MTProto.portHelp")}
        />
        <B4NumberField
          label={t("settings.MTProto.maxConnections")}
          value={config.system.mtproto?.max_connections || 2048}
          onChange={(n) => onChange("system.mtproto.max_connections", n)}
          min={16}
          max={100000}
          disabled={!config.system.mtproto?.enabled}
          helperText={t("settings.MTProto.maxConnectionsHelp")}
        />
        <B4TextField
          label={t("settings.MTProto.fakeSNI")}
          value={config.system.mtproto?.fake_sni || "storage.googleapis.com"}
          onChange={(e) => onChange("system.mtproto.fake_sni", e.target.value)}
          disabled={!config.system.mtproto?.enabled}
          helperText={t("settings.MTProto.fakeSNIHelp")}
        />
      </B4FormGroup>
      <B4FormGroup
        label={t("settings.MTProto.secretsTitle")}
        description={t("settings.MTProto.secretsDesc")}
        columns={1}
      >
        <MTProtoSecrets
          config={config}
          onChange={onChange}
          onShare={openShare}
        />
      </B4FormGroup>
      {(() => {
        const mode = config.system.mtproto?.upstream_mode || "auto";
        const showDcRelay = !!dcRelay || mode === "tcp" || mode === "auto";
        return (
          <B4FormGroup
            label={t("settings.MTProto.upstreamTitle")}
            description={t("settings.MTProto.upstreamDesc")}
            columns={1}
          >
            <Box
              sx={{
                display: "grid",
                gridTemplateColumns: { xs: "1fr", md: "1fr 1fr" },
                gap: 2,
              }}
            >
              <B4Select
                label={t("settings.MTProto.upstreamMode")}
                value={mode}
                onChange={(e) =>
                  onChange(
                    "system.mtproto.upstream_mode",
                    String(e.target.value),
                  )
                }
                options={[
                  { value: "tcp", label: t("settings.MTProto.upstreamTcp") },
                  { value: "auto", label: t("settings.MTProto.upstreamAuto") },
                  { value: "ws", label: t("settings.MTProto.upstreamWs") },
                ]}
                helperText={`${
                  mode === "auto" && dcRelay
                    ? t("settings.MTProto.upstreamAutoRelayDesc")
                    : t(
                        `settings.MTProto.upstream${upstreamDescSuffix(mode)}Desc`,
                      )
                } ${t("settings.MTProto.upstreamBridgeNote")}`}
              />
              {showDcRelay && (
                <B4TextField
                  label={t("settings.MTProto.dcRelay")}
                  value={config.system.mtproto?.dc_relay || ""}
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
                          <Tooltip
                            title={t("settings.MTProto.dcRelayHelpButton")}
                          >
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
              )}
            </Box>
            <B4TextField
              label={t("settings.MTProto.cfWorkerDomain")}
              value={config.system.mtproto?.cfworker_domain || ""}
              onChange={(e) =>
                onChange("system.mtproto.cfworker_domain", e.target.value)
              }
              onBlur={(e) => {
                const cleaned = normalizeWorkerDomains(e.target.value);
                if (cleaned !== e.target.value) {
                  onChange("system.mtproto.cfworker_domain", cleaned);
                }
              }}
              placeholder="my-worker-1234.username.workers.dev"
              helperText={t("settings.MTProto.cfWorkerDomainHelp")}
              selectOnFocus
              slotProps={{
                input: {
                  endAdornment: (
                    <InputAdornment position="end" sx={{ mr: -0.5 }}>
                      <Tooltip title={t("settings.MTProto.cfWorkerSetup")}>
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
            <Box sx={{ display: "flex", flexDirection: "column", gap: 1.5 }}>
              <B4Switch
                label={t("settings.MTProto.cfProxyEnabled")}
                checked={config.system.mtproto?.cfproxy_enabled ?? true}
                onChange={(checked: boolean) =>
                  onChange("system.mtproto.cfproxy_enabled", checked)
                }
                description={t("settings.MTProto.cfProxyEnabledHelp")}
              />
              {config.system.mtproto?.cfproxy_enabled !== false && (
                <B4TextField
                  label={t("settings.MTProto.cfProxyURL")}
                  value={config.system.mtproto?.cfproxy_url || ""}
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
                checked={config.system.mtproto?.dc_fallback_enabled ?? true}
                onChange={(checked: boolean) =>
                  onChange("system.mtproto.dc_fallback_enabled", checked)
                }
                description={t("settings.MTProto.dcFallbackEnabledHelp")}
              />
              {config.system.mtproto?.dc_fallback_enabled !== false && (
                <B4TextField
                  label={t("settings.MTProto.dcFallbackURL")}
                  value={config.system.mtproto?.dc_fallback_url || ""}
                  onChange={(e) =>
                    onChange("system.mtproto.dc_fallback_url", e.target.value)
                  }
                  placeholder="https://proxy.lavrush.in/telegram/getProxyConfig"
                  helperText={t("settings.MTProto.dcFallbackURLHelp")}
                  selectOnFocus
                />
              )}
            </Box>
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
                      label = `${r.transport} — ${parts.join(", ")}`;
                    } else {
                      const stageLabel = r.stage
                        ? t(`settings.MTProto.testStage_${r.stage}`, {
                            defaultValue: r.stage,
                          })
                        : "";
                      label = stageLabel
                        ? `${r.transport} — [${stageLabel}] ${r.error}`
                        : `${r.transport} — ${r.error}`;
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
          </B4FormGroup>
        );
      })()}
      <Typography variant="caption">
        {t("settings.MTProto.credit")}{" "}
        <Link
          href="https://github.com/Flowseal/tg-ws-proxy"
          target="_blank"
          rel="noreferrer"
          sx={{
            display: "inline-flex",
            alignItems: "center",
            gap: 0.25,
          }}
        >
          tg-ws-proxy
          <OpenInNewIcon sx={{ fontSize: 12 }} />
        </Link>
      </Typography>
      <B4Dialog
        open={shareOpen}
        onClose={() => setShareOpen(false)}
        fullWidth
        maxWidth="sm"
        title={t("settings.MTProto.shareDialogTitle")}
        icon={<IosShareIcon />}
        actions={
          <>
            <Button onClick={() => setShareOpen(false)}>
              {t("core.close")}
            </Button>
            <Box sx={{ flex: 1 }} />
            <Button
              component="a"
              variant="outlined"
              href={shareLink || "#"}
              target="_blank"
              rel="noreferrer"
              startIcon={<OpenInNewIcon />}
              disabled={!shareLink}
            >
              {t("settings.MTProto.shareOpen")}
            </Button>
            {canShare && (
              <Button
                variant="contained"
                startIcon={<IosShareIcon />}
                onClick={() => void handleNativeShare()}
                disabled={!shareLink}
              >
                {t("settings.MTProto.shareNative")}
              </Button>
            )}
            <Button
              variant="contained"
              startIcon={copied ? <CheckIcon /> : <ContentCopyIcon />}
              onClick={() => void handleCopy()}
              disabled={!shareLink}
            >
              {copied ? t("core.copied") : t("core.copy")}
            </Button>
          </>
        }
      >
        <B4TextField
          sx={{ mt: 3 }}
          label={t("settings.MTProto.shareHost")}
          value={shareHost}
          onChange={(e) => setShareHost(e.target.value)}
          helperText={t("settings.MTProto.shareHostHelp")}
          autoFocus
        />
        <B4TextField
          label={t("settings.MTProto.shareLinkLabel")}
          value={shareLink}
          slotProps={{
            input: {
              readOnly: true,
              endAdornment: (
                <InputAdornment position="end">
                  <Tooltip title={copied ? t("core.copied") : t("core.copy")}>
                    <span>
                      <IconButton
                        size="small"
                        onClick={() => void handleCopy()}
                        disabled={!shareLink}
                      >
                        {copied ? (
                          <CheckIcon fontSize="small" color="success" />
                        ) : (
                          <ContentCopyIcon fontSize="small" />
                        )}
                      </IconButton>
                    </span>
                  </Tooltip>
                </InputAdornment>
              ),
            },
          }}
          helperText={t("settings.MTProto.shareLinkHelp")}
        />
        {shareLink && (
          <Box
            sx={{
              display: "flex",
              flexDirection: "column",
              alignItems: "center",
              gap: 1,
              alignSelf: "center",
            }}
          >
            <Box sx={{ px: 1, pt: 1, bgcolor: "#fff", borderRadius: 2 }}>
              <QRCodeSVG
                value={shareLink}
                size={220}
                level="H"
                marginSize={0}
                imageSettings={{
                  src: "/favicon.svg",
                  height: 32,
                  width: 32,
                  excavate: true,
                }}
              />
            </Box>
            <Typography variant="caption" color="text.secondary">
              {t("settings.MTProto.shareQrHelp")}
            </Typography>
          </Box>
        )}
      </B4Dialog>
      <MTProtoRelayHelpDialog
        open={relayHelpOpen}
        onClose={() => setRelayHelpOpen(false)}
        relayInfo={relayInfo}
        refreshResult={refreshResult}
        refreshing={refreshing}
        onRefresh={() => void handleRefreshDCs()}
      />
    </B4Section>
  );
};
