import { Box, Stack, TextField } from "@mui/material";
import { ClearIcon } from "@b4.icons";
import { B4Badge, B4Switch, B4TooltipButton } from "@b4.elements";
import { colors } from "@design";
import { useTranslation } from "react-i18next";

interface DomainsControlBarProps {
  filter: string;
  onFilterChange: (filter: string) => void;
  totalCount: number;
  filteredCount: number;
  sortColumn: string | null;
  showAll: boolean;
  onShowAllChange: (showAll: boolean) => void;
  onClearSort: () => void;
  onReset: () => void;
}

export const DomainsControlBar = ({
  filter,
  onFilterChange,
  totalCount,
  filteredCount,
  sortColumn,
  showAll,
  onShowAllChange,
  onClearSort,
  onReset,
}: DomainsControlBarProps) => {
  const { t } = useTranslation();

  return (
    <Box
      sx={{
        p: 2,
        borderBottom: "1px solid",
        borderColor: colors.border.light,
        bgcolor: colors.background.control,
      }}
    >
      <Stack
        direction="row"
        spacing={2}
        alignItems="center"
        flexWrap="wrap"
        useFlexGap
      >
        <TextField
          size="small"
          placeholder={t("connections.controlBar.filterPlaceholder")}
          value={filter}
          onChange={(e) => onFilterChange(e.target.value)}
          sx={{ flex: 1 }}
          slotProps={{
            input: {
              sx: {
                bgcolor: colors.background.dark,
                "& fieldset": {
                  borderColor: `${colors.border.default} !important`,
                },
              },
            },
          }}
        />
        <Stack direction="row" spacing={1} alignItems="center">
          <B4Badge label={t("connections.controlBar.connections", { count: totalCount })} />
          {filter && (
            <B4Badge label={t("core.filtered", { count: filteredCount })} variant="outlined" />
          )}
          {sortColumn && (
            <B4Badge
              label={t("connections.controlBar.sortedBy", { column: sortColumn })}
              size="small"
              onDelete={onClearSort}
              variant="outlined"
              color="primary"
            />
          )}
        </Stack>
        <B4Switch
          label={showAll ? t("connections.controlBar.allPackets") : t("connections.controlBar.domainsOnly")}
          checked={showAll}
          onChange={(checked: boolean) => onShowAllChange(checked)}
        />
        <B4TooltipButton
          title={t("connections.controlBar.clearConnections")}
          onClick={onReset}
          icon={<ClearIcon />}
        />
      </Stack>
    </Box>
  );
};
