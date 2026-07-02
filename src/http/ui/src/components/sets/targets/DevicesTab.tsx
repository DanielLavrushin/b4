import {
  Box,
  Button,
  Checkbox,
  Chip,
  CircularProgress,
  Grid,
  Paper,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Typography,
} from "@mui/material";
import { ClearIcon, RefreshIcon } from "@b4.icons";
import { B4Alert, B4Badge, B4Hint, B4TooltipButton } from "@b4.elements";
import { DeviceInfo } from "@b4.devices";
import { colors } from "@design";
import { sortDevices } from "@utils";
import { useTranslation } from "react-i18next";

interface DevicesTabProps {
  selected: string[];
  devices: DeviceInfo[];
  loading: boolean;
  available: boolean;
  onRefresh: () => void;
  onChange: (macs: string[]) => void;
}

export const DevicesTab = ({
  selected,
  devices,
  loading,
  available,
  onRefresh,
  onChange,
}: DevicesTabProps) => {
  const { t } = useTranslation();

  const isSelected = (mac: string) => selected.includes(mac);

  const handleToggle = (mac: string) => {
    onChange(
      isSelected(mac) ? selected.filter((m) => m !== mac) : [...selected, mac],
    );
  };

  return (
    <>
      <B4Hint>{t("sets.targets.deviceAlert")}</B4Hint>

      {available ? (
        <Grid container spacing={2}>
          <Grid size={{ xs: 12 }}>
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
                    ({t("sets.targets.selectedCount", { count: selected.length })}
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

            <TableContainer
              component={Paper}
              sx={{
                bgcolor: colors.background.paper,
                border: `1px solid ${colors.border.default}`,
                maxHeight: 350,
              }}
            >
              <Table size="small" stickyHeader>
                <TableHead>
                  <TableRow>
                    <TableCell
                      padding="checkbox"
                      sx={{ bgcolor: colors.background.dark }}
                    >
                      <Checkbox
                        color="secondary"
                        indeterminate={
                          selected.length > 0 &&
                          selected.length < devices.length
                        }
                        checked={
                          devices.length > 0 &&
                          selected.length === devices.length
                        }
                        onChange={(e) =>
                          onChange(
                            e.target.checked ? devices.map((d) => d.mac) : [],
                          )
                        }
                      />
                    </TableCell>
                    {[
                      t("core.devices.macAddress"),
                      t("core.devices.ip"),
                      t("core.devices.deviceName"),
                    ].map((label) => (
                      <TableCell
                        key={label}
                        sx={{
                          bgcolor: colors.background.dark,
                          color: colors.text.secondary,
                        }}
                      >
                        {label}
                      </TableCell>
                    ))}
                  </TableRow>
                </TableHead>
                <TableBody>
                  {devices.length === 0 ? (
                    <TableRow>
                      <TableCell colSpan={4} align="center">
                        {loading
                          ? t("core.devices.loadingDevices")
                          : t("core.devices.noDevices")}
                      </TableCell>
                    </TableRow>
                  ) : (
                    sortDevices(devices, isSelected).map((device) => (
                      <TableRow
                        key={device.mac}
                        hover
                        onClick={() => handleToggle(device.mac)}
                        sx={{ cursor: "pointer" }}
                      >
                        <TableCell padding="checkbox">
                          <Checkbox
                            checked={isSelected(device.mac)}
                            color="secondary"
                            onChange={(event) => {
                              event.stopPropagation();
                              handleToggle(device.mac);
                            }}
                          />
                        </TableCell>
                        <TableCell
                          sx={{ fontFamily: "monospace", fontSize: "0.85rem" }}
                        >
                          {device.is_manual ? (
                            <Typography
                              variant="caption"
                              color="text.secondary"
                            >
                              —
                            </Typography>
                          ) : (
                            device.mac
                          )}
                        </TableCell>
                        <TableCell
                          sx={{ fontFamily: "monospace", fontSize: "0.85rem" }}
                        >
                          <Box
                            sx={{
                              display: "flex",
                              alignItems: "center",
                              gap: 0.5,
                            }}
                          >
                            {device.ip}
                            {device.is_manual && (
                              <Chip
                                label={t("core.devices.manual")}
                                size="small"
                                variant="outlined"
                                sx={{ fontSize: "0.7rem", height: 20 }}
                              />
                            )}
                          </Box>
                        </TableCell>
                        <TableCell>
                          <B4Badge
                            label={
                              device.alias ||
                              device.vendor ||
                              device.hostname ||
                              t("core.unknown")
                            }
                            color="primary"
                            variant={
                              isSelected(device.mac) ? "filled" : "outlined"
                            }
                          />
                        </TableCell>
                      </TableRow>
                    ))
                  )}
                </TableBody>
              </Table>
            </TableContainer>

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
