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
import { macKey, offlineDeviceInfo } from "@utils";
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

const withUnlistedSelection = (
  devices: DeviceInfo[],
  selected: string[],
): DeviceInfo[] => {
  const known = new Set(devices.map((d) => macKey(d.mac)));
  const unlisted: DeviceInfo[] = [];
  for (const mac of selected) {
    const key = macKey(mac);
    if (known.has(key)) continue;
    known.add(key);
    unlisted.push(offlineDeviceInfo(mac));
  }
  return unlisted.length > 0 ? [...devices, ...unlisted] : devices;
};

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
  const leafSets = useMemo(() => {
    if (
      !devicesLeaf ||
      !Array.isArray(devicesLeaf.after) ||
      !Array.isArray(devicesLeaf.before)
    ) {
      return null;
    }
    return {
      after: new Set((devicesLeaf.after as unknown[]).map((m) => macKey(String(m)))),
      before: new Set((devicesLeaf.before as unknown[]).map((m) => macKey(String(m)))),
    };
  }, [devicesLeaf]);
  const isDeviceChanged = (mac: string): boolean => {
    const key = macKey(mac);
    return !!leafSets && leafSets.after.has(key) !== leafSets.before.has(key);
  };

  const selectedKeys = useMemo(() => new Set(selected.map(macKey)), [selected]);
  const isSelected = (mac: string) => selectedKeys.has(macKey(mac));

  const handleToggle = (mac: string) => {
    const key = macKey(mac);
    onChange(
      selectedKeys.has(key)
        ? selected.filter((m) => macKey(m) !== key)
        : [...selected, mac],
    );
  };

  const handleBulkToggle = (macs: string[], checked: boolean) => {
    const keys = new Set(macs.map(macKey));
    if (!checked) {
      onChange(selected.filter((m) => !keys.has(macKey(m))));
      return;
    }
    const seen = new Set(selectedKeys);
    const added = macs.filter((m) => {
      const key = macKey(m);
      if (seen.has(key)) return false;
      seen.add(key);
      return true;
    });
    onChange([...selected, ...added]);
  };

  const hasMssHints = devices.some((d) => d.mss_clamp);
  const listed =
    loading && devices.length === 0
      ? devices
      : withUnlistedSelection(devices, selected);

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
              devices={listed}
              loading={loading}
              isSelected={isSelected}
              onToggle={handleToggle}
              isChanged={isDeviceChanged}
              onSelectAll={(checked) =>
                onChange(checked ? listed.map((d) => d.mac) : [])
              }
              onBulkToggle={handleBulkToggle}
              showOfflineChip
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
