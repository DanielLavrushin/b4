import {
  Box,
  Button,
  Chip,
  CircularProgress,
  Grid,
  Typography,
} from "@mui/material";
import { ClearIcon, RefreshIcon } from "@b4.icons";
import { B4Alert, B4Hint, B4Switch, B4TooltipButton } from "@b4.elements";
import { DeviceInfo } from "@b4.devices";
import { B4DeviceTable } from "@common/B4DeviceTable";
import { colors } from "@design";
import { useTranslation } from "react-i18next";

interface DevicesTabProps {
  selected: string[];
  exclude: boolean;
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
  devices,
  loading,
  available,
  onRefresh,
  onChange,
  onExcludeChange,
}: DevicesTabProps) => {
  const { t } = useTranslation();

  const isSelected = (mac: string) => selected.includes(mac);

  const handleToggle = (mac: string) => {
    onChange(
      isSelected(mac) ? selected.filter((m) => m !== mac) : [...selected, mac],
    );
  };

  const hasMssHints = devices.some((d) => d.mss_clamp);

  return (
    <>
      <B4Hint>{t("sets.targets.deviceAlert")}</B4Hint>

      {available ? (
        <Grid container spacing={2}>
          <Grid size={{ xs: 12 }}>
            <Box sx={{ mt: 2, maxWidth: 480 }}>
              <B4Switch
                label={t("sets.targets.excludeDevices")}
                description={t("sets.targets.excludeDevicesDesc")}
                checked={exclude}
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
              <B4TooltipButton
                title={t("core.devices.refreshDevices")}
                icon={
                  loading ? <CircularProgress size={18} /> : <RefreshIcon />
                }
                onClick={onRefresh}
              />
            </Box>

            <B4DeviceTable
              devices={devices}
              loading={loading}
              isSelected={isSelected}
              onToggle={handleToggle}
              onSelectAll={(checked) =>
                onChange(checked ? devices.map((d) => d.mac) : [])
              }
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

            {selected.length > 0 && (
              <Box sx={{ mt: 2 }}>
                <Button
                  size="small"
                  onClick={() => onChange([])}
                  startIcon={<ClearIcon />}
                >
                  {t("core.clearAll")}
                </Button>
              </Box>
            )}
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
