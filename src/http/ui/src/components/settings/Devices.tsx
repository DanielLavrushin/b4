import { useState, useEffect } from "react";
import {
  Grid,
  Box,
  Typography,
  Chip,
  CircularProgress,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Paper,
  IconButton,
  TextField,
  Button,
} from "@mui/material";
import { useTranslation } from "react-i18next";
import { Device } from "@models/config";
import { DeviceUnknowIcon, RefreshIcon, AddIcon, DeleteIcon } from "@b4.icons";
import EditIcon from "@mui/icons-material/Edit";
import { colors } from "@design";
import {
  B4Section,
  B4Switch,
  B4Alert,
  B4TooltipButton,
  B4Badge,
  B4InlineEdit,
  B4Hint,
} from "@b4.elements";
import { useDevices, DevicesSettingsProps } from "@b4.devices";
import { B4DeviceTable } from "@common/B4DeviceTable";

const toMac = (bytes: number[]): string =>
  `02:B4:${bytes.map((b) => b.toString(16).toUpperCase().padStart(2, "0")).join(":")}`;

const parseIPv4 = (ip: string): number[] | null => {
  const parts = ip.split(".");
  if (parts.length !== 4) return null;
  const octets: number[] = [];
  for (const p of parts) {
    if (!/^\d+$/.test(p)) return null;
    const n = Number(p);
    if (n < 0 || n > 255) return null;
    octets.push(n);
  }
  return octets;
};

const splitIPv6 = (ip: string): { head: string[]; tail: string[] } | null => {
  const dblIdx = ip.indexOf("::");
  if (dblIdx < 0) return { head: ip.split(":"), tail: [] };
  if (ip.slice(dblIdx + 1).includes("::")) return null;
  const left = ip.slice(0, dblIdx);
  const right = ip.slice(dblIdx + 2);
  return {
    head: left ? left.split(":") : [],
    tail: right ? right.split(":") : [],
  };
};

const extractEmbeddedIPv4 = (
  head: string[],
  tail: string[],
): number[] | null | undefined => {
  const last = tail.length ? tail.at(-1) : head.at(-1);
  if (!last?.includes(".")) return undefined;
  const v4 = parseIPv4(last);
  if (!v4) return null;
  if (tail.length) tail.pop();
  else head.pop();
  return v4;
};

const groupsToBytes = (groups: string[]): number[] | null => {
  const bytes: number[] = [];
  for (const g of groups) {
    if (!/^[0-9a-fA-F]{1,4}$/.test(g)) return null;
    const n = Number.parseInt(g, 16);
    bytes.push((n >> 8) & 0xff, n & 0xff);
  }
  return bytes;
};

const parseIPv6 = (raw: string): number[] | null => {
  const zoneIdx = raw.indexOf("%");
  const ip = zoneIdx >= 0 ? raw.slice(0, zoneIdx) : raw;
  if (!ip.includes(":")) return null;
  const split = splitIPv6(ip);
  if (!split) return null;
  const { head, tail } = split;
  const embedded = extractEmbeddedIPv4(head, tail);
  if (embedded === null) return null;
  const groupsCount = 8 - (embedded ? 2 : 0);
  const fillCount = groupsCount - head.length - tail.length;
  const hasDouble = ip.includes("::");
  if (fillCount < 0 || (!hasDouble && fillCount !== 0)) return null;
  const groups = [...head, ...new Array<string>(fillCount).fill("0"), ...tail];
  const bytes = groupsToBytes(groups);
  if (!bytes) return null;
  if (embedded) bytes.push(...embedded);
  return bytes.length === 16 ? bytes : null;
};

const generateSyntheticMAC = (ip: string): string => {
  const trimmed = ip.trim();
  const v4 = parseIPv4(trimmed);
  if (v4) return toMac(v4);
  const v6 = parseIPv6(trimmed);
  if (v6) return toMac(v6.slice(12));
  return "";
};

export const DevicesSettings = ({ config, onChange }: DevicesSettingsProps) => {
  const [editingMac, setEditingMac] = useState<string | null>(null);
  const [manualIp, setManualIp] = useState("");
  const [manualName, setManualName] = useState("");
  const { t } = useTranslation();

  const configDevices: Device[] = config.queue.devices?.devices || [];
  const enabled = config.queue.devices?.enabled || false;
  const vendorLookup = config.queue.devices?.vendor_lookup || false;
  const wisb = config.queue.devices?.wisb || false;
  const { devices, loading, available, source, loadDevices } = useDevices();

  useEffect(() => {
    loadDevices().catch(() => {});
  }, [loadDevices]);

  const findConfigDevice = (mac: string): Device | undefined =>
    configDevices.find((d) => d.mac.toUpperCase() === mac.toUpperCase());

  const updateDevice = (mac: string, update: Partial<Device>) => {
    const current = [...configDevices];
    const idx = current.findIndex(
      (d) => d.mac.toUpperCase() === mac.toUpperCase(),
    );
    if (idx === -1) {
      current.push({ mac: mac.toUpperCase(), selected: false, ...update });
    } else {
      current[idx] = { ...current[idx], ...update };
    }
    const cleaned = current.filter(
      (d) =>
        d.selected || d.is_manual || (d.mss_clamp && d.mss_clamp > 0) || d.name,
    );
    onChange("queue.devices.devices", cleaned);
  };

  const handleToggle = (mac: string) => {
    const existing = findConfigDevice(mac);
    updateDevice(mac, { selected: !existing?.selected });
  };

  const handleSelectAll = (selectAll: boolean) => {
    const visibleMacs = new Set(devices.map((d) => d.mac.toUpperCase()));
    const updated = configDevices.map((d) => {
      if (selectAll && !visibleMacs.has(d.mac.toUpperCase())) return d;
      if (!selectAll && d.is_manual) return d;
      return { ...d, selected: selectAll };
    });
    if (selectAll) {
      for (const d of devices) {
        if (!updated.some((u) => u.mac.toUpperCase() === d.mac.toUpperCase())) {
          updated.push({ mac: d.mac.toUpperCase(), selected: true });
        }
      }
    }
    const cleaned = updated.filter(
      (d) =>
        d.selected || d.is_manual || (d.mss_clamp && d.mss_clamp > 0) || d.name,
    );
    onChange("queue.devices.devices", cleaned);
  };

  const handleAddManualDevice = () => {
    const ip = manualIp.trim();
    if (!ip) return;
    const mac = generateSyntheticMAC(ip);
    if (!mac) return;
    if (configDevices.some((d) => d.mac.toUpperCase() === mac.toUpperCase()))
      return;
    const updated = [
      ...configDevices,
      {
        mac: mac.toUpperCase(),
        ip,
        name: manualName.trim() || undefined,
        selected: false,
        is_manual: true,
      },
    ];
    onChange("queue.devices.devices", updated);
    setManualIp("");
    setManualName("");
  };

  const handleRemoveManualDevice = (mac: string) => {
    onChange(
      "queue.devices.devices",
      configDevices.filter((d) => d.mac.toUpperCase() !== mac.toUpperCase()),
    );
  };

  const isSelected = (mac: string) => findConfigDevice(mac)?.selected || false;
  const manualDevices = configDevices.filter((d) => d.is_manual);

  return (
    <B4Section
      title={t("settings.Devices.title")}
      description={t("settings.Devices.description")}
      icon={<DeviceUnknowIcon />}
    >
      <Grid container spacing={2}>
        <Grid size={{ xs: 12 }}>
          <Box
            sx={{
              display: "flex",
              gap: 3,
              alignItems: "center",
              flexWrap: "wrap",
            }}
          >
            <B4Switch
              label={t("settings.Devices.enable")}
              checked={enabled}
              onChange={(checked) => onChange("queue.devices.enabled", checked)}
              description={t("settings.Devices.enableDesc")}
            />
            <B4Switch
              label={t("settings.Devices.vendorLookup")}
              checked={vendorLookup}
              onChange={(checked) =>
                onChange("queue.devices.vendor_lookup", checked)
              }
              description={t("settings.Devices.vendorLookupDesc")}
            />
            <B4Switch
              label={t("settings.Devices.invertSelection")}
              checked={wisb}
              onChange={(checked) => onChange("queue.devices.wisb", checked)}
              description={
                wisb
                  ? t("settings.Devices.invertBlacklist")
                  : t("settings.Devices.invertWhitelist")
              }
              disabled={!enabled}
            />
          </Box>
        </Grid>

        {enabled && (
          <>
            <B4Alert severity={wisb ? "warning" : "info"}>
              {wisb
                ? t("settings.Devices.blacklistAlert")
                : t("settings.Devices.whitelistAlert")}
            </B4Alert>

            {available ? (
              <Grid size={{ xs: 12 }}>
                <Box
                  sx={{
                    display: "flex",
                    justifyContent: "space-between",
                    alignItems: "center",
                    mb: 1,
                  }}
                >
                  <Typography variant="subtitle2">
                    {t("core.devices.availableDevices")}
                    {source && (
                      <Chip
                        label={source}
                        size="small"
                        sx={{
                          ml: 1,
                          bgcolor: colors.accent.secondary,
                          color: colors.secondary,
                        }}
                      />
                    )}
                  </Typography>
                  <B4TooltipButton
                    title={t("core.devices.refreshDevices")}
                    icon={
                      loading ? <CircularProgress size={18} /> : <RefreshIcon />
                    }
                    onClick={() => {
                      loadDevices().catch(() => {});
                    }}
                  />
                </Box>

                <B4DeviceTable
                  devices={devices}
                  loading={loading}
                  isSelected={isSelected}
                  onToggle={handleToggle}
                  onSelectAll={handleSelectAll}
                  showOfflineChip
                  maxHeight={300}
                  renderNameCell={(device) =>
                    editingMac === device.mac ? (
                      <B4InlineEdit
                        value={
                          findConfigDevice(device.mac)?.name ||
                          device.alias ||
                          device.vendor ||
                          ""
                        }
                        onSave={(name) => {
                          updateDevice(device.mac, { name });
                          setEditingMac(null);
                        }}
                        onCancel={() => setEditingMac(null)}
                      />
                    ) : (
                      <Box
                        sx={{
                          display: "flex",
                          alignItems: "center",
                          gap: 0.5,
                        }}
                      >
                        {findConfigDevice(device.mac)?.name ||
                        device.alias ||
                        device.vendor ? (
                          <B4Badge
                            label={
                              findConfigDevice(device.mac)?.name ||
                              device.alias ||
                              device.vendor ||
                              ""
                            }
                            color="primary"
                            variant={
                              isSelected(device.mac) ? "filled" : "outlined"
                            }
                          />
                        ) : (
                          <Typography
                            variant="caption"
                            color="text.secondary"
                          >
                            {t("core.unknown")}
                          </Typography>
                        )}
                        <IconButton
                          size="small"
                          onClick={() => setEditingMac(device.mac)}
                          sx={{
                            opacity: 0.6,
                            "&:hover": { opacity: 1 },
                          }}
                        >
                          <EditIcon sx={{ fontSize: 16 }} />
                        </IconButton>
                      </Box>
                    )
                  }
                  extraColumns={[
                    {
                      header: t("settings.Devices.mss"),
                      renderCell: (device) => (
                        <TextField
                          size="small"
                          type="number"
                          value={findConfigDevice(device.mac)?.mss_clamp || ""}
                          onChange={(e) => {
                            const val =
                              e.target.value === ""
                                ? undefined
                                : Number(e.target.value);
                            updateDevice(device.mac, { mss_clamp: val });
                          }}
                          placeholder="off"
                          slotProps={{
                            htmlInput: {
                              min: 10,
                              max: 1460,
                              style: { width: 50, padding: "4px 8px" },
                            },
                          }}
                          sx={{
                            "& .MuiOutlinedInput-root": {
                              fontSize: "0.85rem",
                            },
                          }}
                        />
                      ),
                    },
                  ]}
                />
              </Grid>
            ) : (
              <B4Alert severity="warning">
                {t("settings.Devices.arpUnavailable")}
              </B4Alert>
            )}

            <Grid size={{ xs: 12 }}>
              <Typography variant="subtitle2" sx={{ mt: 2, mb: 1 }}>
                {t("settings.Devices.manualDevices")}
              </Typography>
              <B4Hint>{t("settings.Devices.manualDevicesDesc")}</B4Hint>

              <Box
                sx={{
                  display: "flex",
                  gap: 1,
                  alignItems: "center",
                  mt: 2,
                  mb: 1,
                }}
              >
                <TextField
                  size="small"
                  label={t("settings.Devices.manualIp")}
                  value={manualIp}
                  onChange={(e) => setManualIp(e.target.value)}
                  placeholder="192.168.1.100"
                  onKeyDown={(e) => {
                    if (e.key === "Enter") handleAddManualDevice();
                  }}
                  sx={{ minWidth: 160 }}
                />
                <TextField
                  size="small"
                  label={t("settings.Devices.manualName")}
                  value={manualName}
                  onChange={(e) => setManualName(e.target.value)}
                  placeholder={t("settings.Devices.manualNamePlaceholder")}
                  onKeyDown={(e) => {
                    if (e.key === "Enter") handleAddManualDevice();
                  }}
                  sx={{ minWidth: 160 }}
                />
                <Button
                  variant="outlined"
                  size="small"
                  onClick={handleAddManualDevice}
                  disabled={!manualIp.trim()}
                  startIcon={<AddIcon />}
                >
                  {t("core.add")}
                </Button>
              </Box>

              {manualDevices.length > 0 && (
                <TableContainer
                  component={Paper}
                  sx={{
                    bgcolor: colors.background.paper,
                    border: `1px solid ${colors.border.default}`,
                    maxHeight: 200,
                  }}
                >
                  <Table size="small" stickyHeader>
                    <TableHead>
                      <TableRow>
                        <TableCell
                          sx={{
                            bgcolor: colors.background.dark,
                            color: colors.text.secondary,
                          }}
                        >
                          {t("core.devices.ip")}
                        </TableCell>
                        <TableCell
                          sx={{
                            bgcolor: colors.background.dark,
                            color: colors.text.secondary,
                          }}
                        >
                          {t("core.devices.deviceName")}
                        </TableCell>
                        <TableCell
                          sx={{ bgcolor: colors.background.dark, width: 50 }}
                        />
                      </TableRow>
                    </TableHead>
                    <TableBody>
                      {manualDevices.map((d) => (
                        <TableRow key={d.mac}>
                          <TableCell
                            sx={{
                              fontFamily: "monospace",
                              fontSize: "0.85rem",
                            }}
                          >
                            {d.ip}
                          </TableCell>
                          <TableCell>
                            {d.name ? (
                              <B4Badge
                                label={d.name}
                                color="primary"
                                variant="outlined"
                              />
                            ) : (
                              <Typography
                                variant="caption"
                                color="text.secondary"
                              >
                                —
                              </Typography>
                            )}
                          </TableCell>
                          <TableCell>
                            <IconButton
                              size="small"
                              onClick={() => handleRemoveManualDevice(d.mac)}
                              sx={{
                                color: colors.text.secondary,
                                "&:hover": { color: "error.main" },
                              }}
                            >
                              <DeleteIcon sx={{ fontSize: 18 }} />
                            </IconButton>
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </TableContainer>
              )}
            </Grid>
          </>
        )}
      </Grid>
    </B4Section>
  );
};
