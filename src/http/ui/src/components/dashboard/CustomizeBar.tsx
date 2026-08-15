import { Box, Button, Chip, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";
import { colors, radiusPx } from "@design";
import { AddIcon, RestoreIcon } from "@b4.icons";

export interface HiddenPanelEntry {
  id: string;
  title: string;
  available: boolean;
}

interface CustomizeBarProps {
  editing: boolean;
  customized: boolean;
  hiddenPanels: HiddenPanelEntry[];
  onShow: (id: string) => void;
  onReset: () => void;
}

export const CustomizeBar = ({
  editing,
  customized,
  hiddenPanels,
  onShow,
  onReset,
}: CustomizeBarProps) => {
  const { t } = useTranslation();

  if (!editing) return null;

  return (
    <Box
      sx={{
        mb: 1.5,
        p: "10px 12px",
        border: `1px solid ${colors.border.strong}`,
        borderRadius: `${radiusPx.md}px`,
        bgcolor: colors.background.control,
        display: "flex",
        flexDirection: "column",
        gap: "8px",
      }}
    >
      <Box
        sx={{
          display: "flex",
          alignItems: "center",
          gap: "12px",
          flexWrap: "wrap",
        }}
      >
        <Typography variant="body2" sx={{ color: colors.text.secondary, flex: 1 }}>
          {t("dashboard.customize.hint")}
        </Typography>
        {customized && (
          <Button
            size="small"
            startIcon={<RestoreIcon sx={{ fontSize: 16 }} />}
            onClick={onReset}
            sx={{ color: colors.text.secondary, textTransform: "none" }}
          >
            {t("dashboard.customize.reset")}
          </Button>
        )}
      </Box>

      {hiddenPanels.length > 0 && (
        <Box
          sx={{
            display: "flex",
            alignItems: "center",
            gap: "8px",
            flexWrap: "wrap",
          }}
        >
          <Typography
            variant="metricLabel"
            sx={{ color: colors.text.secondary, opacity: 0.8 }}
          >
            {t("dashboard.customize.hidden")}
          </Typography>
          {hiddenPanels.map((panel) => (
            <Chip
              key={panel.id}
              size="small"
              variant="outlined"
              icon={<AddIcon sx={{ fontSize: 14 }} />}
              label={
                panel.available
                  ? panel.title
                  : `${panel.title} (${t("dashboard.customize.unavailable")})`
              }
              onClick={() => onShow(panel.id)}
              sx={{ opacity: panel.available ? 1 : 0.6 }}
            />
          ))}
        </Box>
      )}
    </Box>
  );
};
