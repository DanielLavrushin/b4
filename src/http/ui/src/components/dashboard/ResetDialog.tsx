import { useState } from "react";
import { Box, Button, Stack, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";
import { colors } from "@design";
import { B4Dialog } from "@common/B4Dialog";
import { formatClock } from "@common/charts";
import { apiPost } from "@api/apiClient";
import { useSnackbar } from "@context/SnackbarProvider";
import { describeApiError } from "@utils";

interface ResetCountersResult {
  success: boolean;
  stats_since?: number;
}

export const resetCounters = () =>
  apiPost<ResetCountersResult>("/api/metrics/reset");

const CLEARS = ["blocked", "domains", "rst", "escalations", "connections"] as const;
const KEEPS = ["activity", "uptime", "events", "escalations"] as const;

interface ResetDialogProps {
  open: boolean;
  onClose: () => void;
}

function ListBlock({
  title,
  items,
}: {
  title: string;
  items: readonly string[];
}) {
  return (
    <Box sx={{ mt: 1.5 }}>
      <Typography
        component="h3"
        sx={{
          m: 0,
          fontSize: 12,
          fontWeight: 700,
          letterSpacing: "0.08em",
          textTransform: "uppercase",
          color: colors.text.secondary,
        }}
      >
        {title}
      </Typography>
      <Box
        component="ul"
        sx={{
          m: 0,
          mt: 0.5,
          pl: 2.5,
          fontSize: 14,
          lineHeight: 1.55,
          color: colors.text.primary,
        }}
      >
        {items.map((item) => (
          <li key={item}>{item}</li>
        ))}
      </Box>
    </Box>
  );
}

export function ResetDialog({ open, onClose }: ResetDialogProps) {
  const { t, i18n } = useTranslation();
  const { showSuccess, showError } = useSnackbar();
  const [busy, setBusy] = useState(false);

  const run = async () => {
    setBusy(true);
    try {
      const result = await resetCounters();
      const since =
        typeof result.stats_since === "number" && result.stats_since > 0
          ? result.stats_since
          : Date.now();
      showSuccess(
        t("dashboard.reset.done", {
          time: formatClock(since, i18n.language, true),
        }),
      );
      onClose();
    } catch (error) {
      showError(t("dashboard.reset.failed", { error: describeApiError(error) }));
    } finally {
      setBusy(false);
    }
  };

  return (
    <B4Dialog
      open={open}
      onClose={() => {
        if (!busy) onClose();
      }}
      title={t("dashboard.reset.title")}
      maxWidth="sm"
      fullWidth
      actions={
        <Stack direction="row" spacing={1}>
          <Button
            onClick={onClose}
            disabled={busy}
            sx={{ color: colors.text.secondary }}
          >
            {t("core.cancel")}
          </Button>
          <Button
            onClick={() => void run()}
            disabled={busy}
            variant="contained"
            color="warning"
          >
            {t("dashboard.reset.confirm")}
          </Button>
        </Stack>
      }
    >
      <Typography sx={{ color: colors.text.primary, mt: 1, fontSize: 14 }}>
        {t("dashboard.reset.intro")}
      </Typography>
      <ListBlock
        title={t("dashboard.reset.clearsTitle")}
        items={CLEARS.map((key) => t(`dashboard.reset.clears.${key}`))}
      />
      <ListBlock
        title={t("dashboard.reset.keepsTitle")}
        items={KEEPS.map((key) => t(`dashboard.reset.keeps.${key}`))}
      />
    </B4Dialog>
  );
}
