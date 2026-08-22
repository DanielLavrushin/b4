import { Box, Typography, Paper } from "@mui/material";
import { colors } from "@design";
import { B4Alert, B4Badge } from "@b4.elements";
import { useTranslation } from "react-i18next";

interface NoBypassCardProps {
  domain: string;
  speed?: number;
}

export const NoBypassCard = ({ domain, speed }: NoBypassCardProps) => {
  const { t } = useTranslation();

  return (
    <Paper
      elevation={0}
      sx={{
        bgcolor: colors.background.paper,
        border: `1px solid ${colors.border.default}`,
        borderRadius: 2,
        overflow: "hidden",
      }}
    >
      <Box
        sx={{
          px: 2,
          py: 2,
          bgcolor: colors.accent.primary,
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
        }}
      >
        <Box sx={{ display: "flex", alignItems: "center", gap: 2 }}>
          <Typography variant="h6" sx={{ color: colors.text.primary }}>
            {domain}
          </Typography>
          <B4Badge
            label={t("discovery.noBypass.badge")}
            size="small"
            color="success"
          />
        </Box>
        {!!speed && speed > 0 && (
          <Typography
            variant="body2"
            sx={{ color: colors.text.secondary, fontWeight: 600 }}
          >
            {(speed / 1024).toFixed(0)} KB/s
          </Typography>
        )}
      </Box>
      <Box sx={{ p: 2 }}>
        <B4Alert severity="success">{t("discovery.noBypass.body")}</B4Alert>
      </Box>
    </Paper>
  );
};
