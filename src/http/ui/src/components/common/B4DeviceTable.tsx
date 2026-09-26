import { ReactNode, useState } from "react";
import {
  Box,
  Checkbox,
  Chip,
  IconButton,
  InputAdornment,
  Paper,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Typography,
} from "@mui/material";
import { B4Badge, B4TextField } from "@b4.elements";
import { CloseIcon, SearchIcon } from "@b4.icons";
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
  onBulkToggle: (macs: string[], checked: boolean) => void;
  renderNameCell?: (device: DeviceInfo) => ReactNode;
  getSearchableName?: (device: DeviceInfo) => string;
  extraColumns?: B4DeviceTableColumn[];
  showOfflineChip?: boolean;
  maxHeight?: number;
}

const defaultName = (device: DeviceInfo) =>
  device.alias || device.vendor || device.hostname || "";

export const B4DeviceTable = ({
  devices,
  loading,
  isSelected,
  onToggle,
  onSelectAll,
  onBulkToggle,
  renderNameCell,
  getSearchableName = defaultName,
  extraColumns = [],
  showOfflineChip = false,
  maxHeight = 350,
}: B4DeviceTableProps) => {
  const { t } = useTranslation();
  const [filterText, setFilterText] = useState("");

  const query = filterText.trim().toLowerCase();
  const filteredDevices = query
    ? devices.filter((d) =>
        [d.is_manual ? "" : d.mac, d.ip, getSearchableName(d)].some((field) =>
          field.toLowerCase().includes(query),
        ),
      )
    : devices;

  const selectedCount = filteredDevices.filter((d) => isSelected(d.mac)).length;
  const allSelected =
    filteredDevices.length > 0 && selectedCount === filteredDevices.length;
  const someSelected = selectedCount > 0 && !allSelected;

  const handleSelectAll = (checked: boolean) => {
    if (query) {
      onBulkToggle(
        filteredDevices.map((d) => d.mac),
        checked,
      );
    } else {
      onSelectAll(checked);
    }
  };

  const emptyText = () => {
    if (devices.length > 0) return t("core.devices.noMatches");
    return loading
      ? t("core.devices.loadingDevices")
      : t("core.devices.noDevices");
  };

  const headers = [
    t("core.devices.macAddress"),
    t("core.devices.ip"),
    t("core.devices.deviceName"),
    ...extraColumns.map((c) => c.header),
  ];

  return (
    <Box>
      <B4TextField
        label={t("core.devices.filter")}
        placeholder={t("core.devices.filterPlaceholder")}
        value={filterText}
        onChange={(e) => setFilterText(e.target.value)}
        sx={{ mb: 1.5 }}
        slotProps={{
          input: {
            startAdornment: (
              <InputAdornment position="start">
                <SearchIcon fontSize="small" />
              </InputAdornment>
            ),
            endAdornment: filterText ? (
              <InputAdornment position="end">
                <IconButton
                  size="small"
                  onClick={() => setFilterText("")}
                  aria-label={t("core.devices.clearFilter")}
                >
                  <CloseIcon fontSize="small" />
                </IconButton>
              </InputAdornment>
            ) : undefined,
          },
        }}
      />
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
                  onChange={(e) => handleSelectAll(e.target.checked)}
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
            {filteredDevices.length === 0 ? (
              <TableRow>
                <TableCell colSpan={headers.length + 1} align="center">
                  {emptyText()}
                </TableCell>
              </TableRow>
            ) : (
              sortDevices(filteredDevices, isSelected).map((device) => (
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
                        {t("core.devices.byIp")}
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
                        label={defaultName(device) || t("core.unknown")}
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
    </Box>
  );
};
