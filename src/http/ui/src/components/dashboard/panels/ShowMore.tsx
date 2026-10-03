import { Box, Button } from "@mui/material";
import { useTranslation } from "react-i18next";
import { colors } from "@design";
import { formatInteger } from "@common/charts";
import { PANEL_PAD_X } from "./styles";

export const COLLAPSED_ROWS = 8;

interface ShowMoreProps {
  total: number;
  expanded: boolean;
  onToggle: () => void;
  limit?: number;
  controls?: string;
}

export function ShowMore({
  total,
  expanded,
  onToggle,
  limit = COLLAPSED_ROWS,
  controls,
}: ShowMoreProps) {
  const { t, i18n } = useTranslation();
  if (total <= limit) return null;
  return (
    <Box
      sx={{
        px: PANEL_PAD_X,
        py: "6px",
        borderTop: `1px solid ${colors.border.light}`,
      }}
    >
      <Button
        size="small"
        onClick={onToggle}
        aria-expanded={expanded}
        aria-controls={controls}
        sx={{
          px: 1,
          py: "2px",
          minWidth: 0,
          fontSize: 12,
          fontWeight: 600,
          color: colors.secondary,
        }}
      >
        {expanded
          ? t("dashboard.common.showLess")
          : t("dashboard.common.showMore", {
              value: formatInteger(total - limit, i18n.language),
            })}
      </Button>
    </Box>
  );
}

export function visibleRows<T>(rows: readonly T[], expanded: boolean, limit = COLLAPSED_ROWS): readonly T[] {
  return expanded || rows.length <= limit ? rows : rows.slice(0, limit);
}
