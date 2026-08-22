import { ReactNode } from "react";
import {
  Box,
  Checkbox,
  Chip,
  Paper,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Typography,
} from "@mui/material";
import { B4Badge } from "@b4.elements";
import { DeviceInfo } from "@b4.devices";
import { colors } from "@design";
import { sortDevices } from "@utils";
import { useTranslation } from "react-i18next";

export interface B4DeviceTableColumn {
  header: string;
  renderCell: (device: DeviceInfo) => ReactNode;
}

interface B4DeviceTableProps {
  devices: DeviceInfo[];
  loading: boolean;
  isSelected: (mac: string) => boolean;
  onToggle: (mac: string) => void;
  onSelectAll: (checked: boolean) => void;
  renderNameCell?: (device: DeviceInfo) => ReactNode;
  extraColumns?: B4DeviceTableColumn[];
  showOfflineChip?: boolean;
  maxHeight?: number;
}

export const B4DeviceTable = ({
  devices,
  loading,
  isSelected,
  onToggle,
  onSelectAll,
  renderNameCell,
  extraColumns = [],
  showOfflineChip = false,
  maxHeight = 350,
}: B4DeviceTableProps) => {
  const { t } = useTranslation();
  const selectedCount = devices.filter((d) => isSelected(d.mac)).length;
  const allSelected = devices.length > 0 && selectedCount === devices.length;
  const someSelected = selectedCount > 0 && !allSelected;
  const headers = [
    t("core.devices.macAddress"),
    t("core.devices.ip"),
    t("core.devices.deviceName"),
    ...extraColumns.map((c) => c.header),
  ];

  return (
    <TableContainer
      component={Paper}
      sx={{
        bgcolor: colors.background.paper,
        border: `1px solid ${colors.border.default}`,
        maxHeight,
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
                indeterminate={someSelected}
                checked={allSelected}
                onChange={(e) => onSelectAll(e.target.checked)}
              />
            </TableCell>
            {headers.map((label) => (
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
              <TableCell colSpan={headers.length + 1} align="center">
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
                onClick={() => onToggle(device.mac)}
                sx={{ cursor: "pointer" }}
              >
                <TableCell padding="checkbox">
                  <Checkbox
                    checked={isSelected(device.mac)}
                    color="secondary"
                    onChange={(event) => {
                      event.stopPropagation();
                      onToggle(device.mac);
                    }}
                  />
                </TableCell>
                <TableCell
                  sx={{ fontFamily: "monospace", fontSize: "0.85rem" }}
                >
                  {device.is_manual ? (
                    <Typography variant="caption" color="text.secondary">
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
                    sx={{ display: "flex", alignItems: "center", gap: 0.5 }}
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
                    {showOfflineChip &&
                      !device.is_manual &&
                      device.is_online === false && (
                        <Chip
                          label={t("core.devices.offline")}
                          size="small"
                          variant="outlined"
                          sx={{
                            fontSize: "0.7rem",
                            height: 20,
                            color: colors.text.secondary,
                          }}
                        />
                      )}
                  </Box>
                </TableCell>
                {renderNameCell ? (
                  <TableCell onClick={(e) => e.stopPropagation()}>
                    {renderNameCell(device)}
                  </TableCell>
                ) : (
                  <TableCell>
                    <B4Badge
                      label={
                        device.alias ||
                        device.vendor ||
                        device.hostname ||
                        t("core.unknown")
                      }
                      color="primary"
                      variant={isSelected(device.mac) ? "filled" : "outlined"}
                    />
                  </TableCell>
                )}
                {extraColumns.map((col) => (
                  <TableCell
                    key={col.header}
                    onClick={(e) => e.stopPropagation()}
                  >
                    {col.renderCell(device)}
                  </TableCell>
                ))}
              </TableRow>
            ))
          )}
        </TableBody>
      </Table>
    </TableContainer>
  );
};
