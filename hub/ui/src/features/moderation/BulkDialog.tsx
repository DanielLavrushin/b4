import { useEffect, useState } from "react";
import {
  Alert,
  Box,
  Button,
  Checkbox,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
  FormControlLabel,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  TextField,
} from "@mui/material";
import { useTranslation } from "react-i18next";
import { colors } from "@design";
import { useSnackbar } from "@/app/SnackbarProvider";
import type { ModerationItemView, ModerationRequest, SetAction } from "@/models/api";
import { ErrorState } from "@/shared/components/States";
import { setRef } from "@/shared/utils/format";
import { usePreview, useModerate } from "./api";
import { catalogueEffect } from "./EffectText";
import { ReasonPicker } from "./ReasonPicker";
import type { VersionTarget } from "./VersionActionDialog";

interface BulkDialogProps {
  action: SetAction;
  targets: VersionTarget[];
  open: boolean;
  onClose: () => void;
  onDone?: () => void;
}

function EffectCell({ item }: { item: ModerationItemView }) {
  const { t } = useTranslation();
  if (!item.ok) return <Box sx={{ color: colors.state.error }}>{t(`errors.${item.code ?? "invalid"}`, { ...(item.params ?? {}), defaultValue: item.code })}</Box>;
  const effect = catalogueEffect(item);
  return <>{t(effect.key, effect.params)}</>;
}

export function BulkDialog({ action, targets, open, onClose, onDone }: Readonly<BulkDialogProps>) {
  const { t } = useTranslation();
  const { notifyResult, notifyError } = useSnackbar();
  const moderate = useModerate();
  const [reason, setReason] = useState("");
  const [withdraw, setWithdraw] = useState(false);

  useEffect(() => {
    if (open) {
      setReason("");
      setWithdraw(false);
    }
  }, [open]);

  const base: ModerationRequest = {
    action,
    reason: action === "reject" ? "preview" : "",
    items: targets.map((x) => ({ set_id: x.set_id, version: x.version, expect_status: x.status })),
    force: false,
    withdraw,
    keep_reports: false,
    partial: true,
    dry_run: true,
  };
  const preview = usePreview(open && targets.length > 0 ? base : null);
  const items = preview.data?.items ?? [];
  const valid = items.filter((i) => i.ok).length;
  const needsReason = action === "reject" && reason.trim() === "";
  const titles = new Map(targets.map((x) => [setRef(x.set_id, x.version), x.title]));

  const apply = async () => {
    try {
      const result = await moderate.mutateAsync({ ...base, reason, dry_run: false, partial: true });
      notifyResult(result);
      onDone?.();
      onClose();
    } catch (err) {
      notifyError(err);
    }
  };

  return (
    <Dialog open={open} onClose={moderate.isPending ? undefined : onClose} fullWidth maxWidth="md">
      <DialogTitle>{t(`bulk.${action}.title`, { count: targets.length })}</DialogTitle>
      <DialogContent sx={{ display: "flex", flexDirection: "column", gap: 2 }}>
        <DialogContentText>{t(`moderation.${action}.text`)}</DialogContentText>
        {preview.isLoading && <CircularProgress size={20} />}
        {preview.error && <ErrorState error={preview.error} onRetry={() => void preview.refetch()} />}
        {items.length > 0 && (
          <Box sx={{ maxHeight: 320, overflow: "auto", border: `1px solid ${colors.border.light}`, borderRadius: 1 }}>
            <Table size="small" stickyHeader>
              <TableHead>
                <TableRow>
                  <TableCell>{t("bulk.columns.set")}</TableCell>
                  <TableCell>{t("bulk.columns.effect")}</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {items.map((item) => {
                  const ref = setRef(item.set_id, item.version);
                  return (
                    <TableRow key={ref}>
                      <TableCell>
                        {titles.get(ref) ?? item.title}
                        <Box component="span" sx={{ color: colors.text.secondary, fontSize: 11, ml: 1 }}>
                          {ref}
                        </Box>
                      </TableCell>
                      <TableCell>
                        <EffectCell item={item} />
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </Box>
        )}
        {items.length > 0 && valid < items.length && (
          <Alert severity="warning">{t("bulk.someRefused", { valid, total: items.length })}</Alert>
        )}
        {(action === "hide" || action === "reject") && (
          <FormControlLabel control={<Checkbox checked={withdraw} onChange={(e) => setWithdraw(e.target.checked)} />} label={t("moderation.withdrawToo")} />
        )}
        {(action === "hide" || action === "reject") && <ReasonPicker scope={action} value={reason} onPick={setReason} />}
        {(action === "hide" || action === "reject") && (
          <TextField
            fullWidth
            size="small"
            label={t("app.reason")}
            value={reason}
            required={action === "reject"}
            onChange={(e) => setReason(e.target.value)}
            helperText={needsReason ? t("app.reasonRequired") : t("moderation.reasonHint")}
          />
        )}
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose} disabled={moderate.isPending} color="inherit">
          {t("app.cancel")}
        </Button>
        <Button
          variant="contained"
          color={action === "approve" || action === "restore" ? "primary" : "error"}
          disabled={moderate.isPending || needsReason || valid === 0}
          onClick={() => void apply()}
        >
          {t("bulk.apply", { count: valid })}
        </Button>
      </DialogActions>
    </Dialog>
  );
}
