import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  Button,
  Box,
  CircularProgress,
  IconButton,
  InputAdornment,
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
import AutorenewIcon from "@mui/icons-material/Autorenew";
import HelpOutlineIcon from "@mui/icons-material/HelpOutline";
import NetworkPingIcon from "@mui/icons-material/NetworkPing";
import { MTProtoRelayHelpDialog } from "./MTProtoRelayHelpDialog";
import { QRCodeSVG } from "qrcode.react";
import { ConnectionIcon } from "@b4.icons";
import {
  B4FormGroup,
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
  latency_ms?: number;
  error?: string;
};

const upstreamDescSuffix = (mode: string) => {
  if (mode === "tcp") return "Tcp";
  if (mode === "ws") return "Ws";
  return "Auto";
};

interface MTProtoSettingsProps {
  config: B4Config;
  onChange: (field: string, value: SettingsPropHandlerType) => void;
}

export const MTProtoSettings = ({ config, onChange }: MTProtoSettingsProps) => {
  const { t } = useTranslation();
  const [generating, setGenerating] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [refreshResult, setRefreshResult] = useState<
    | { ok: true; count: number; dcs: Record<string, string> }
    | { ok: false; error: string }
    | null
  >(null);
  const [shareOpen, setShareOpen] = useState(false);
  const [shareHost, setShareHost] = useState("");
  const [copied, setCopied] = useState(false);
  const [relayHelpOpen, setRelayHelpOpen] = useState(false);
  const [wsTesting, setWsTesting] = useState(false);
  const [wsResults, setWsResults] = useState<WsProbeResult[] | null>(null);
  const [wsTestError, setWsTestError] = useState<string | null>(null);

  const port = config.system.mtproto?.port ?? 3128;
  const secret = config.system.mtproto?.secret || "";
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
    if (!host || !secret) return "";
    return `tg://proxy?server=${encodeURIComponent(host)}&port=${port}&secret=${encodeURIComponent(secret)}`;
  }, [shareHost, port, secret]);
  const canShare =
    typeof navigator !== "undefined" && typeof navigator.share === "function";

  const openShare = () => {
    const bind = config.system.mtproto?.bind_address || "";
    const isAnyAddr = !bind || bind === "0.0.0.0" || bind === "::";
    setShareHost(isAnyAddr ? globalThis.location.hostname : bind);
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

  const handleTestWS = async () => {
    setWsTesting(true);
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
          dc: 2,
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
      setWsTesting(false);
    }
  };

  const handleGenerateSecret = async () => {
    const sni = config.system.mtproto?.fake_sni || "storage.googleapis.com";
    setGenerating(true);
    try {
      const res = await fetch("/api/mtproto/generate-secret", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ fake_sni: sni }),
      });
      const data = (await res.json()) as { success: boolean; secret?: string };
      if (data.success && data.secret) {
        onChange("system.mtproto.secret", data.secret);
      }
    } finally {
      setGenerating(false);
    }
  };

  return (
    <B4Section
      title={t("settings.MTProto.title")}
      description={t("settings.MTProto.description")}
      icon={<ConnectionIcon />}
    >
      <B4FormGroup label={t("settings.MTProto.settings")} columns={2}>
        <B4Switch
          label={t("settings.MTProto.enable")}
          checked={config.system.mtproto?.enabled ?? false}
          onChange={(checked: boolean) =>
            onChange("system.mtproto.enabled", checked)
          }
          description={t("settings.MTProto.enableDesc")}
        />
        {config.system.mtproto?.enabled && (
          <B4Alert severity="warning">
            {t("settings.MTProto.restartNote")}
          </B4Alert>
        )}
        <B4TextField
          label={t("settings.MTProto.bindAddress")}
          value={config.system.mtproto?.bind_address || "0.0.0.0"}
          onChange={(e) =>
            onChange("system.mtproto.bind_address", e.target.value)
          }
          placeholder={t("settings.MTProto.bindAddressPlaceholder")}
          disabled={!config.system.mtproto?.enabled}
          helperText={t("settings.MTProto.bindAddressHelp")}
        />
        <B4TextField
          label={t("settings.MTProto.port")}
          type="number"
          value={config.system.mtproto?.port ?? 3128}
          onChange={(e) =>
            onChange("system.mtproto.port", Number(e.target.value))
          }
          disabled={!config.system.mtproto?.enabled}
          helperText={t("settings.MTProto.portHelp")}
        />
        <B4TextField
          label={t("settings.MTProto.fakeSNI")}
          value={config.system.mtproto?.fake_sni || "storage.googleapis.com"}
          onChange={(e) => onChange("system.mtproto.fake_sni", e.target.value)}
          disabled={!config.system.mtproto?.enabled}
          helperText={t("settings.MTProto.fakeSNIHelp")}
        />
        <Box sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
          <B4TextField
            label={t("settings.MTProto.secret")}
            value={config.system.mtproto?.secret || ""}
            onChange={(e) => onChange("system.mtproto.secret", e.target.value)}
            disabled={!config.system.mtproto?.enabled}
            helperText={t("settings.MTProto.secretHelp")}
            autoComplete="off"
            slotProps={{
              input: {
                endAdornment: (
                  <InputAdornment position="end">
                    <Chip
                      size="small"
                      icon={
                        <AutorenewIcon
                          sx={{
                            animation: generating
                              ? "spin 1s linear infinite"
                              : "none",
                            "@keyframes spin": {
                              from: { transform: "rotate(0deg)" },
                              to: { transform: "rotate(360deg)" },
                            },
                          }}
                        />
                      }
                      label={
                        generating
                          ? t("settings.MTProto.generating")
                          : t("settings.MTProto.generateSecret")
                      }
                      onClick={() => void handleGenerateSecret()}
                      disabled={!config.system.mtproto?.enabled || generating}
                      sx={{ cursor: "pointer" }}
                    />
                  </InputAdornment>
                ),
              },
            }}
          />
          <Button
            variant="contained"
            size="small"
            startIcon={<IosShareIcon />}
            onClick={openShare}
            disabled={!config.system.mtproto?.enabled || !secret}
            sx={{ alignSelf: "flex-start" }}
          >
            {t("settings.MTProto.shareLink")}
          </Button>
        </Box>
      </B4FormGroup>
      {(() => {
        const mode = config.system.mtproto?.upstream_mode || "auto";
        const enabled = !!config.system.mtproto?.enabled;
        const showDcRelay =
          !!dcRelay || mode === "tcp" || mode === "auto";
        return (
          <B4FormGroup
            label={t("settings.MTProto.upstreamTitle")}
            description={t("settings.MTProto.upstreamDesc")}
            columns={2}
          >
            <B4Select
              label={t("settings.MTProto.upstreamMode")}
              value={mode}
              onChange={(e) =>
                onChange("system.mtproto.upstream_mode", String(e.target.value))
              }
              disabled={!enabled}
              options={[
                { value: "tcp", label: t("settings.MTProto.upstreamTcp") },
                { value: "auto", label: t("settings.MTProto.upstreamAuto") },
                { value: "ws", label: t("settings.MTProto.upstreamWs") },
              ]}
              helperText={t(
                `settings.MTProto.upstream${upstreamDescSuffix(mode)}Desc`,
              )}
            />
            {showDcRelay && (
              <B4TextField
                label={t("settings.MTProto.dcRelay")}
                value={config.system.mtproto?.dc_relay || ""}
                onChange={(e) =>
                  onChange("system.mtproto.dc_relay", e.target.value)
                }
                placeholder="vps-ip:7007"
                disabled={!enabled}
                helperText={t("settings.MTProto.dcRelayHelp")}
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
                              disabled={!enabled}
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
            {mode !== "tcp" && (
              <B4TextField
                label={t("settings.MTProto.wsCustomDomain")}
                value={config.system.mtproto?.ws_custom_domain || ""}
                onChange={(e) =>
                  onChange("system.mtproto.ws_custom_domain", e.target.value)
                }
                placeholder="your-domain.com"
                disabled={!enabled}
                helperText={t("settings.MTProto.wsCustomDomainHelp")}
              />
            )}
            {mode !== "tcp" && (
              <B4TextField
                label={t("settings.MTProto.wsEndpointHost")}
                value={config.system.mtproto?.ws_endpoint_host || ""}
                onChange={(e) =>
                  onChange("system.mtproto.ws_endpoint_host", e.target.value)
                }
                placeholder="149.154.167.220"
                disabled={!enabled}
                helperText={t("settings.MTProto.wsEndpointHostHelp")}
              />
            )}
            <Box sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
              <Button
                variant="outlined"
                size="small"
                startIcon={
                  wsTesting ? (
                    <CircularProgress size={14} />
                  ) : (
                    <NetworkPingIcon fontSize="small" />
                  )
                }
                onClick={() => void handleTestWS()}
                disabled={!config.system.mtproto?.enabled || wsTesting}
                sx={{ alignSelf: "flex-start" }}
              >
                {wsTesting
                  ? t("settings.MTProto.testWsRunning")
                  : t("settings.MTProto.testWs")}
              </Button>
              {wsTestError && <B4Alert severity="error">{wsTestError}</B4Alert>}
              {wsResults && (
                <Stack spacing={0.5}>
                  {wsResults.map((r) => (
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
                      label={
                        r.ok
                          ? `${r.transport} — ${r.latency_ms} ms`
                          : `${r.transport} — ${r.error}`
                      }
                      sx={{
                        justifyContent: "flex-start",
                        maxWidth: "100%",
                        "& .MuiChip-label": {
                          overflow: "hidden",
                          textOverflow: "ellipsis",
                        },
                      }}
                    />
                  ))}
                </Stack>
              )}
            </Box>
          </B4FormGroup>
        );
      })()}
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
