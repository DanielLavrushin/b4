import { Box, Stack, TextField, ToggleButton, ToggleButtonGroup } from "@mui/material";
import { ClearIcon } from "@b4.icons";
import { B4Switch, B4TooltipButton } from "@b4.elements";
import { colors } from "@design";
import { useTranslation } from "react-i18next";

export type TimeWindow = 30 | 60 | 300 | 900 | 0;

interface Props {
  filter: string;
  onFilterChange: (v: string) => void;
  window: TimeWindow;
  onWindowChange: (w: TimeWindow) => void;
  unmatchedOnly: boolean;
  onUnmatchedOnlyChange: (v: boolean) => void;
  showAll: boolean;
  onShowAllChange: (v: boolean) => void;
  onReset: () => void;
}

export const AggregatedControlBar = ({
  filter,
  onFilterChange,
  window,
  onWindowChange,
  unmatchedOnly,
  onUnmatchedOnlyChange,
  showAll,
  onShowAllChange,
  onReset,
}: Props) => {
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
      <Stack direction="row" spacing={2} alignItems="center" flexWrap="wrap" useFlexGap>
        <TextField
          size="small"
          placeholder={t("connections.controlBar.filterPlaceholder")}
          value={filter}
          onChange={(e) => onFilterChange(e.target.value)}
          sx={{ flex: 1, minWidth: 220 }}
          slotProps={{
            input: {
              sx: {
                bgcolor: colors.background.dark,
                "& fieldset": { borderColor: `${colors.border.default} !important` },
              },
            },
          }}
        />

        <ToggleButtonGroup
          size="small"
          exclusive
          value={window}
          onChange={(_, v) => v !== null && onWindowChange(v as TimeWindow)}
        >
          <ToggleButton value={30}>30s</ToggleButton>
          <ToggleButton value={60}>1m</ToggleButton>
          <ToggleButton value={300}>5m</ToggleButton>
          <ToggleButton value={900}>15m</ToggleButton>
          <ToggleButton value={0}>{t("connections.aggregated.windowAll")}</ToggleButton>
        </ToggleButtonGroup>

        <B4Switch
          label={t("connections.aggregated.unmatchedOnly")}
          checked={unmatchedOnly}
          onChange={onUnmatchedOnlyChange}
        />
        <B4Switch
          label={showAll ? t("connections.controlBar.allPackets") : t("connections.controlBar.domainsOnly")}
          checked={showAll}
          onChange={onShowAllChange}
        />

        <B4TooltipButton
          title={t("connections.controlBar.clearPackets")}
          onClick={onReset}
          icon={<ClearIcon />}
        />
      </Stack>
    </Box>
  );
};
