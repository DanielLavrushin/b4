import { useState } from "react";
import {
  Box,
  Button,
  CircularProgress,
  DialogContent,
  Stack,
} from "@mui/material";
import { useTranslation } from "react-i18next";
import { B4Dialog, B4TextField } from "@b4.elements";
import { ReportIcon } from "@b4.icons";
import { HubSet } from "@models/hub";
import { describeHubError } from "@utils";
import { useSnackbar } from "@context/SnackbarProvider";
import { useHubReport } from "@hooks/useHub";

const REASON_LIMIT = 500;

interface ReportDialogProps {
  set: HubSet;
  onClose: () => void;
}

export const ReportDialog = ({ set, onClose }: ReportDialogProps) => {
  const { t } = useTranslation();
  const { showSuccess, showError } = useSnackbar();
  const [reason, setReason] = useState("");
  const report = useHubReport();
  const trimmed = reason.trim();

  const send = () => {
    if (!trimmed) return;
    report.mutate(
      { id: set.id, reason: trimmed },
      {
        onSuccess: (res) => {
          showSuccess(
            res.sent ? t("hub.report.sent") : t("hub.report.queued"),
          );
          onClose();
        },
        onError: (e) =>
          showError(t("hub.report.failed", { error: describeHubError(e, t) })),
      },
    );
  };

  return (
    <B4Dialog
      open
      onClose={onClose}
      maxWidth="sm"
      fullWidth
      icon={<ReportIcon />}
      title={t("hub.report.title", { name: set.title })}
      subtitle={t("hub.report.subtitle")}
      actions={
        <>
          <Button onClick={onClose} disabled={report.isPending}>
            {t("core.cancel")}
          </Button>
          <Box sx={{ flex: 1 }} />
          <Button
            variant="contained"
            startIcon={
              report.isPending ? (
                <CircularProgress size={14} color="inherit" />
              ) : (
                <ReportIcon />
              )
            }
            disabled={report.isPending || !trimmed}
            onClick={send}
          >
            {t("hub.report.send")}
          </Button>
        </>
      }
    >
      <DialogContent sx={{ p: 0 }}>
        <Stack spacing={2}>
          <B4TextField
            label={t("hub.report.reason")}
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            multiline
            minRows={3}
            maxRows={8}
            autoFocus
            disabled={report.isPending}
            helperText={t("hub.report.reasonHelp", { limit: REASON_LIMIT })}
            slotProps={{ htmlInput: { maxLength: REASON_LIMIT } }}
          />
        </Stack>
      </DialogContent>
    </B4Dialog>
  );
};
