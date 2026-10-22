import {
  Box,
  Button,
  Chip,
  CircularProgress,
  Grid,
  Stack,
  Typography,
} from "@mui/material";
import { ClearIcon, RefreshIcon } from "@b4.icons";
import { B4Alert, B4Hint, B4Switch, B4TooltipButton } from "@b4.elements";
import { DeviceInfo } from "@b4.devices";
import { B4DeviceTable } from "@common/B4DeviceTable";
import { colors } from "@design";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { useChangedFields } from "@context/ChangedFieldsContext";
interface DevicesTabProps {
  selected: string[];
  exclude: boolean;
  routingEnabled: boolean;
  hasDestination: boolean;
  devices: DeviceInfo[];
  loading: boolean;
  available: boolean;
  onRefresh: () => void;
  onChange: (macs: string[]) => void;
  onExcludeChange: (exclude: boolean) => void;
}

export const DevicesTab = ({
  selected,
  exclude,
  routingEnabled,
  hasDestination,
  devices,
  loading,
  available,
  onRefresh,
  onChange,
  onExcludeChange,
}: DevicesTabProps) => {
  const { t } = useTranslation();
  const { changedValues } = useChangedFields();
  const devicesLeaf = changedValues.get("targets.source_devices");
  // O(1) membership per row instead of O(n) includes scans.
  const leafSets = useMemo(() => {
    if (
      !devicesLeaf ||
      !Array.isArray(devicesLeaf.after) ||
      !Array.isArray(devicesLeaf.before)
    ) {
      return null;
    }
    return {
      after: new Set(devicesLeaf.after as unknown[]),
      before: new Set(devicesLeaf.before as unknown[]),
    };
  }, [devicesLeaf]);
  const isDeviceChanged = (mac: string): boolean =>
    !!leafSets && leafSets.after.has(mac) !== leafSets.before.has(mac);

  const selectedSet = useMemo(() => new Set(selected), [selected]);
  const isSelected = (mac: string) => selectedSet.has(mac);

  const handleToggle = (mac: string) => {
    onChange(
      isSelected(mac) ? selected.filter((m) => m !== mac) : [...selected, mac],
    );
  };

  const handleBulkToggle = (macs: string[], checked: boolean) => {
    const macSet = new Set(macs);
    onChange(
      checked
        ? Array.from(new Set([...selected, ...macs]))
        : selected.filter((m) => !macSet.has(m)),
    );
  };

  const hasMssHints = devices.some((d) => d.mss_clamp);

  const selectedManual: DeviceInfo[] = [];
  const manualWithoutIp: DeviceInfo[] = [];
  for (const d of devices) {
    if (!d.is_manual || !isSelected(d.mac)) continue;
    selectedManual.push(d);
    if (!d.ip.trim()) manualWithoutIp.push(d);
  }
  const showNoDestination =
    routingEnabled && !hasDestination && selected.length > 0 && !exclude;

  return (
    <>
      <B4Hint>{t("sets.targets.deviceAlert")}</B4Hint>

      {showNoDestination && (
        <Box sx={{ mt: 2 }}>
          <B4Alert severity="warning">
            {t("sets.targets.deviceNoDestination")}
          </B4Alert>
        </Box>
      )}

      {selectedManual.length > 0 && (
        <Box sx={{ mt: 2 }}>
          <B4Alert severity="info">
            {t("sets.targets.manualDeviceInfo")}
          </B4Alert>
        </Box>
      )}

      {manualWithoutIp.length > 0 && (
        <Box sx={{ mt: 2 }}>
          <B4Alert severity="warning">
            {t("sets.targets.manualDeviceNoIp", {
              names: manualWithoutIp
                .map((d) => d.alias?.trim() || d.hostname?.trim() || d.mac)
                .join(", "),
            })}
          </B4Alert>
        </Box>
      )}

      {available ? (
        <Grid container spacing={2}>
          <Grid size={{ xs: 12 }}>
            <Box sx={{ mt: 2, maxWidth: 480 }}>
              <B4Switch
                label={t("sets.targets.excludeDevices")}
                description={t("sets.targets.excludeDevicesDesc")}
                checked={exclude}
                path="targets.source_devices_exclude"
                onChange={onExcludeChange}
              />
            </Box>

            <Box
              sx={{
                display: "flex",
                justifyContent: "space-between",
                alignItems: "center",
                mb: 1,
                mt: 2,
              }}
            >
              <Typography variant="subtitle2">
                {t("core.devices.availableDevices")}
                {selected.length > 0 && (
                  <Typography
                    component="span"
                    variant="caption"
                    sx={{ ml: 1, color: colors.secondary }}
                  >
                    (
                    {t(
                      exclude
                        ? "sets.targets.excludedCount"
                        : "sets.targets.selectedCount",
                      { count: selected.length },
                    )}
                    )
                  </Typography>
                )}
              </Typography>
              <Stack direction="row" spacing={1} alignItems="center">
                {selected.length > 0 && (
                  <Button
                    size="small"
                    onClick={() => onChange([])}
                    startIcon={<ClearIcon />}
                  >
                    {t("core.clearAll")}
                  </Button>
                )}
                <B4TooltipButton
                  title={t("core.devices.refreshDevices")}
                  icon={
                    loading ? <CircularProgress size={18} /> : <RefreshIcon />
                  }
                  onClick={onRefresh}
                />
              </Stack>
            </Box>

            <B4DeviceTable
              devices={devices}
              loading={loading}
              isSelected={isSelected}
              onToggle={handleToggle}
              isChanged={isDeviceChanged}
              onSelectAll={(checked) =>
                onChange(checked ? devices.map((d) => d.mac) : [])
              }
              onBulkToggle={handleBulkToggle}
              extraColumns={
                hasMssHints
                  ? [
                      {
                        header: t("settings.Devices.mss"),
                        renderCell: (device) =>
                          device.mss_clamp ? (
                            <Chip
                              label={device.mss_clamp}
                              size="small"
                              variant="outlined"
                              sx={{ fontSize: "0.7rem", height: 20 }}
                            />
                          ) : null,
                      },
                    ]
                  : []
              }
            />

          </Grid>
        </Grid>
      ) : (
        <Box sx={{ mt: 2 }}>
          <B4Alert severity="warning">
            {t("sets.targets.arpUnavailable")}
          </B4Alert>
        </Box>
      )}
    </>
  );
};
