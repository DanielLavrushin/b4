import { Alert, Box, CircularProgress, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";
import { colors } from "@design";

export function Loading() {
  return (
    <Box sx={{ display: "flex", justifyContent: "center", py: 6 }}>
      <CircularProgress size={28} />
    </Box>
  );
}

export function EmptyState({ text }: { text: string }) {
  return (
    <Typography variant="body2" sx={{ color: colors.text.secondary, py: 2 }}>
      {text}
    </Typography>
  );
}

export function ErrorState({ error }: { error: unknown }) {
  const { t } = useTranslation();
  const message = error instanceof Error ? error.message : String(error);
  return <Alert severity="error">{t("app.error", { message })}</Alert>;
}
