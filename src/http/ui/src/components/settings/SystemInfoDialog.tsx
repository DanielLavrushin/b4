import { useState, useEffect } from "react";
import {
  Button,
  Box,
  Typography,
  CircularProgress,
  Stack,
  Chip,
} from "@mui/material";
import { useTranslation } from "react-i18next";
import i18n from "@/i18n";
import {
  InfoIcon,
  CheckIcon,
  ErrorIcon,
  WarningIcon,
  CopyIcon,
} from "@b4.icons";
import { colors, spacing } from "@design";
import { B4Dialog } from "@common/B4Dialog";
import { useSnackbar } from "@context/SnackbarProvider";
import { copyText } from "@utils";

interface SystemInfoDialogProps {
  open: boolean;
  onClose: () => void;
}

interface DiagSystem {
  hostname: string;
  distro?: string;
  os: string;
  arch: string;
  kernel: string;
  cpu_cores: number;
  mem_total_mb: number;
  mem_avail_mb: number;
  is_docker: boolean;
}

interface DiagB4 {
  version: string;
  commit: string;
  build_date: string;
  service_manager: string;
  config_path: string;
  running: boolean;
  pid?: number;
  memory_mb?: string;
  uptime?: string;
}

interface DiagModule {
  name: string;
  status: string;
}

interface DiagTool {
  name: string;
  found: boolean;
  detail?: string;
}

interface DiagMount {
  path: string;
  available: string;
  writable: boolean;
}

interface DiagInterface {
  name: string;
  mac?: string;
  addrs?: string[];
  up: boolean;
  mtu: number;
}

interface DiagFirewall {
  backend: string;
  nfqueue_works: boolean;
  flow_offload: string;
  active_rules?: string[];
}

interface DiagGeodata {
  geosite_configured: boolean;
  geosite_path?: string;
  geosite_size?: string;
  geoip_configured: boolean;
  geoip_path?: string;
  geoip_size?: string;
  total_domains: number;
  total_ips: number;
}

interface DiagPaths {
  binary: string;
  config: string;
  error_log?: string;
  geosite?: string;
  geoip?: string;
  data_dir?: string;
}

interface Diagnostics {
  system: DiagSystem;
  b4: DiagB4;
  kernel: { modules: DiagModule[] };
  tools: { firewall: DiagTool[]; required: DiagTool[]; optional: DiagTool[] };
  network: { interfaces: DiagInterface[] };
  firewall: DiagFirewall;
  geodata: DiagGeodata;
  storage: DiagMount[];
  paths: DiagPaths;
}

export const SystemInfoDialog = ({ open, onClose }: SystemInfoDialogProps) => {
  const { t } = useTranslation();
  const { showSuccess } = useSnackbar();
  const [data, setData] = useState<Diagnostics | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const copyJson = async () => {
    if (!data) return;
    const ok = await copyText(JSON.stringify(data, null, 2));
    if (ok) showSuccess(t("settings.SystemInfo.copied"));
    else setError(t("settings.SystemInfo.copyFailed"));
  };

  useEffect(() => {
    if (!open) return;
    setData(null);
    setLoading(true);
    setError(null);
    fetch("/api/system/diagnostics")
      .then((r) => r.json())
      .then((json: { success: boolean; data: Diagnostics }) => {
        if (json.success) setData(json.data);
        else setError(i18n.t("settings.SystemInfo.loadFailed"));
      })
      .catch(() => setError(i18n.t("settings.SystemInfo.connectFailed")))
      .finally(() => setLoading(false));
  }, [open]);

  const statusChip = (status: string) => {
    const isOk = status === "loaded" || status === "built-in";
    return (
      <Chip
        size="small"
        label={status}
        icon={isOk ? <CheckIcon /> : <WarningIcon />}
        sx={{
          bgcolor: isOk ? `${colors.secondary}22` : `${colors.quaternary}22`,
          color: isOk ? colors.secondary : colors.quaternary,
          fontSize: "0.7rem",
          height: 22,
        }}
      />
    );
  };

  const boolChip = (ok: boolean, yesLabel: string, noLabel: string) => (
    <Chip
      size="small"
      label={ok ? yesLabel : noLabel}
      icon={ok ? <CheckIcon /> : <ErrorIcon />}
      sx={{
        bgcolor: ok ? `${colors.secondary}22` : `${colors.quaternary}22`,
        color: ok ? colors.secondary : colors.quaternary,
        fontSize: "0.7rem",
        height: 22,
      }}
    />
  );

  const row = (label: string, value: React.ReactNode) => (
    <Stack
      direction="row"
      justifyContent="space-between"
      alignItems="center"
      sx={{
        py: 0.5,
        px: 1,
        "&:nth-of-type(odd)": { bgcolor: `${colors.background.dark}88` },
        borderRadius: 1,
      }}
    >
      <Typography
        variant="caption"
        sx={{ color: colors.text.secondary, minWidth: 140 }}
      >
        {label}
      </Typography>
      <Typography
        variant="caption"
        sx={{
          color: colors.text.primary,
          textAlign: "right",
          wordBreak: "break-all",
        }}
      >
        {value}
      </Typography>
    </Stack>
  );

  const monoRow = (label: string, value: string) => (
    <Stack
      direction="row"
      justifyContent="space-between"
      alignItems="center"
      sx={{
        py: 0.5,
        px: 1,
        "&:nth-of-type(odd)": { bgcolor: `${colors.background.dark}88` },
        borderRadius: 1,
      }}
    >
      <Typography
        variant="caption"
        sx={{ color: colors.text.secondary, minWidth: 100 }}
      >
        {label}
      </Typography>
      <Typography
        variant="caption"
        sx={{
          color: colors.text.primary,
          fontFamily: "monospace",
          fontSize: "0.7rem",
          textAlign: "right",
          wordBreak: "break-all",
        }}
      >
        {value}
      </Typography>
    </Stack>
  );

  const sectionTitle = (title: string) => (
    <Typography
      variant="subtitle2"
      sx={{
        color: colors.secondary,
        mt: 2,
        mb: 0.5,
        fontWeight: 600,
        textTransform: "uppercase",
        fontSize: "0.7rem",
        letterSpacing: 1,
      }}
    >
      {title}
    </Typography>
  );

  const listRow = (name: string, right: React.ReactNode) => (
    <Stack
      direction="row"
      justifyContent="space-between"
      alignItems="center"
      sx={{ py: 0.3, px: 1 }}
    >
      <Typography
        variant="caption"
        sx={{ color: colors.text.primary, fontFamily: "monospace" }}
      >
        {name}
      </Typography>
      {right}
    </Stack>
  );

  return (
    <B4Dialog
      title={t("settings.SystemInfo.title")}
      subtitle={t("settings.SystemInfo.subtitle")}
      icon={<InfoIcon />}
      open={open}
      onClose={onClose}
      maxWidth="md"
      fullWidth
      actions={
        <>
          {data && (
            <Button
              size="small"
              startIcon={<CopyIcon />}
              onClick={() => void copyJson()}
            >
              {t("settings.SystemInfo.copyJson")}
            </Button>
          )}
          <Box sx={{ flex: 1 }} />
          <Button onClick={onClose} variant="contained">
            {t("core.close")}
          </Button>
        </>
      }
    >
      {loading && (
        <Stack alignItems="center" sx={{ py: 4 }}>
          <CircularProgress size={32} sx={{ color: colors.secondary }} />
        </Stack>
      )}

      {error && (
        <Typography sx={{ color: colors.quaternary, py: 2 }}>
          {error}
        </Typography>
      )}

      {data && !loading && (
        <Box sx={{ mt: 1 }}>
          {sectionTitle(t("settings.SystemInfo.system"))}
          {row(t("settings.SystemInfo.hostname"), data.system.hostname)}
          {data.system.distro &&
            row(t("settings.SystemInfo.distro"), data.system.distro)}
          {row(
            t("settings.SystemInfo.os"),
            `${data.system.os} / ${data.system.arch}`,
          )}
          {row(t("settings.SystemInfo.kernel"), data.system.kernel)}
          {row(
            t("settings.SystemInfo.cpu"),
            `${data.system.cpu_cores} ${t("settings.SystemInfo.cores")}`,
          )}
          {row(
            t("settings.SystemInfo.memory"),
            `${data.system.mem_avail_mb} / ${data.system.mem_total_mb} MB`,
          )}
          {data.system.is_docker &&
            row(t("settings.SystemInfo.container"), "Docker")}

          {sectionTitle("B4")}
          {row(
            t("settings.SystemInfo.version"),
            `${data.b4.version} (${data.b4.commit.substring(0, 7)})`,
          )}
          {row(t("settings.SystemInfo.buildDate"), data.b4.build_date)}
          {row(
            t("settings.SystemInfo.serviceManager"),
            data.b4.service_manager,
          )}
          {!!data.b4.pid && row("PID", data.b4.pid)}
          {data.b4.memory_mb &&
            row(t("settings.SystemInfo.memUsage"), `${data.b4.memory_mb} MB`)}
          {data.b4.uptime &&
            row(t("settings.SystemInfo.uptime"), data.b4.uptime)}

          {sectionTitle(t("settings.SystemInfo.paths"))}
          {monoRow(t("settings.SystemInfo.binary"), data.paths.binary)}
          {monoRow(t("settings.SystemInfo.config"), data.paths.config)}
          {data.paths.data_dir &&
            monoRow(t("settings.SystemInfo.dataDir"), data.paths.data_dir)}
          {data.paths.error_log &&
            monoRow(t("settings.SystemInfo.errorLog"), data.paths.error_log)}
          {data.paths.geosite && monoRow("geosite.dat", data.paths.geosite)}
          {data.paths.geoip && monoRow("geoip.dat", data.paths.geoip)}

          {sectionTitle(t("settings.SystemInfo.geodata"))}
          {row(
            "geosite.dat",
            data.geodata.geosite_configured
              ? `${data.geodata.geosite_size}`
              : t("settings.SystemInfo.notConfigured"),
          )}
          {row(
            "geoip.dat",
            data.geodata.geoip_configured
              ? `${data.geodata.geoip_size}`
              : t("settings.SystemInfo.notConfigured"),
          )}
          {row(
            t("settings.SystemInfo.totalDomains"),
            data.geodata.total_domains.toLocaleString(),
          )}
          {row(
            t("settings.SystemInfo.totalIPs"),
            data.geodata.total_ips.toLocaleString(),
          )}

          {sectionTitle(t("settings.SystemInfo.firewall"))}
          {row(t("settings.SystemInfo.fwBackend"), data.firewall.backend)}
          {row("NFQUEUE", boolChip(data.firewall.nfqueue_works, "OK", "FAIL"))}
          {row(
            t("settings.SystemInfo.flowOffload"),
            boolChip(
              data.firewall.flow_offload === "off",
              t("settings.SystemInfo.flowOffloadOff"),
              data.firewall.flow_offload === "hardware"
                ? t("settings.SystemInfo.flowOffloadHw")
                : t("settings.SystemInfo.flowOffloadSw"),
            ),
          )}
          {data.firewall.active_rules &&
            data.firewall.active_rules.length > 0 && (
              <Box
                sx={{
                  mt: 0.5,
                  px: 1,
                  py: 0.5,
                  bgcolor: `${colors.background.dark}88`,
                  borderRadius: 1,
                  maxHeight: 150,
                  overflow: "auto",
                }}
              >
                {data.firewall.active_rules.map((rule) => (
                  <Typography
                    key={rule}
                    variant="caption"
                    sx={{
                      color: colors.text.secondary,
                      fontFamily: "monospace",
                      fontSize: "0.65rem",
                      display: "block",
                      lineHeight: 1.6,
                    }}
                  >
                    {rule}
                  </Typography>
                ))}
              </Box>
            )}

          {sectionTitle(t("settings.SystemInfo.network"))}
          {data.network.interfaces?.map((iface) => (
            <Stack key={iface.name} sx={{ py: 0.3, px: 1 }}>
              <Stack
                direction="row"
                justifyContent="space-between"
                alignItems="center"
              >
                <Stack direction="row" spacing={spacing.sm} alignItems="center">
                  <Typography
                    variant="caption"
                    sx={{
                      color: colors.text.primary,
                      fontFamily: "monospace",
                      fontWeight: 600,
                    }}
                  >
                    {iface.name}
                  </Typography>
                  <Chip
                    size="small"
                    label={iface.up ? "UP" : "DOWN"}
                    sx={{
                      bgcolor: iface.up
                        ? `${colors.secondary}22`
                        : `${colors.quaternary}22`,
                      color: iface.up ? colors.secondary : colors.quaternary,
                      fontSize: "0.65rem",
                      height: 18,
                    }}
                  />
                </Stack>
                <Typography
                  variant="caption"
                  sx={{ color: colors.text.secondary, fontSize: "0.65rem" }}
                >
                  {iface.mac && `${iface.mac} · `}MTU {iface.mtu}
                </Typography>
              </Stack>
              {iface.addrs && iface.addrs.length > 0 && (
                <Typography
                  variant="caption"
                  sx={{
                    color: colors.text.secondary,
                    fontFamily: "monospace",
                    fontSize: "0.65rem",
                    pl: 1,
                  }}
                >
                  {iface.addrs.join(", ")}
                </Typography>
              )}
            </Stack>
          ))}

          {sectionTitle(t("settings.SystemInfo.kernelModules"))}
          {data.kernel.modules.map((m) =>
            listRow(m.name, statusChip(m.status)),
          )}

          {sectionTitle(t("settings.SystemInfo.firewallTools"))}
          {data.tools.firewall.map((tool) =>
            listRow(
              tool.name,
              <Stack direction="row" spacing={spacing.sm} alignItems="center">
                {tool.found && tool.detail && (
                  <Typography
                    variant="caption"
                    sx={{ color: colors.text.secondary, fontSize: "0.65rem" }}
                  >
                    {tool.detail}
                  </Typography>
                )}
                {boolChip(tool.found, "found", "—")}
              </Stack>,
            ),
          )}

          {sectionTitle(t("settings.SystemInfo.requiredTools"))}
          {data.tools.required.map((tool) =>
            listRow(
              tool.name,
              <Stack direction="row" spacing={spacing.sm} alignItems="center">
                {!tool.found && tool.detail && (
                  <Typography
                    variant="caption"
                    sx={{ color: colors.text.secondary, fontSize: "0.65rem" }}
                  >
                    {tool.detail}
                  </Typography>
                )}
                {boolChip(tool.found, "found", "missing")}
              </Stack>,
            ),
          )}

          {sectionTitle(t("settings.SystemInfo.optionalTools"))}
          {data.tools.optional.map((tool) =>
            listRow(
              tool.name,
              <Stack direction="row" spacing={spacing.sm} alignItems="center">
                {!tool.found && tool.detail && (
                  <Typography
                    variant="caption"
                    sx={{ color: colors.text.secondary, fontSize: "0.65rem" }}
                  >
                    {tool.detail}
                  </Typography>
                )}
                {boolChip(tool.found, "found", "missing")}
              </Stack>,
            ),
          )}

          {data.storage.length > 0 && (
            <>
              {sectionTitle(t("settings.SystemInfo.storage"))}
              {data.storage.map((m) =>
                listRow(
                  m.path,
                  <Stack
                    direction="row"
                    spacing={spacing.sm}
                    alignItems="center"
                  >
                    <Typography
                      variant="caption"
                      sx={{ color: colors.text.secondary }}
                    >
                      {m.available}
                    </Typography>
                    <Chip
                      size="small"
                      label={m.writable ? "rw" : "ro"}
                      sx={{
                        bgcolor: m.writable
                          ? `${colors.secondary}22`
                          : `${colors.quaternary}22`,
                        color: m.writable
                          ? colors.secondary
                          : colors.quaternary,
                        fontSize: "0.7rem",
                        height: 22,
                      }}
                    />
                  </Stack>,
                ),
              )}
            </>
          )}
        </Box>
      )}
    </B4Dialog>
  );
};
