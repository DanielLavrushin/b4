import { Alert, Box, Button, CircularProgress, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";
import { colors } from "@design";
import { errorText } from "@/shared/utils/notices";

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

interface ErrorStateProps {
  error: unknown;
  onRetry?: () => void;
  compact?: boolean;
}

export function ErrorState({ error, onRetry, compact }: ErrorStateProps) {
  const { t } = useTranslation();
  return (
    <Alert
      severity={compact ? "warning" : "error"}
      variant={compact ? "outlined" : "standard"}
      action={
        onRetry ? (
          <Button color="inherit" size="small" onClick={onRetry}>
            {t("app.retry")}
          </Button>
        ) : undefined
      }
    >
      {t(compact ? "app.staleData" : "app.error", { message: errorText(t, error) })}
    </Alert>
  );
}
