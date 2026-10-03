import { Box, Button, Chip, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";
import { colors, radiusPx } from "@design";
import { AddIcon, CheckIcon, RestoreIcon } from "@b4.icons";

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
  onDone: () => void;
}

export const CustomizeBar = ({
  editing,
  customized,
  hiddenPanels,
  onShow,
  onReset,
  onDone,
}: CustomizeBarProps) => {
  const { t } = useTranslation();

  if (!editing) return null;

  return (
    <Box
      component="section"
      aria-label={t("dashboard.customize.label")}
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
          gap: "8px 12px",
          flexWrap: "wrap",
        }}
      >
        <Typography
          variant="body2"
          sx={{ color: colors.text.secondary, flex: "1 1 280px" }}
        >
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
        <Button
          size="small"
          variant="contained"
          startIcon={<CheckIcon sx={{ fontSize: 16 }} />}
          onClick={onDone}
          sx={{ textTransform: "none" }}
        >
          {t("dashboard.customize.done")}
        </Button>
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
          <Box
            component="span"
            sx={{
              fontSize: 11,
              fontWeight: 600,
              letterSpacing: "0.1em",
              textTransform: "uppercase",
              color: colors.text.secondary,
            }}
          >
            {t("dashboard.customize.hidden")}
          </Box>
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
