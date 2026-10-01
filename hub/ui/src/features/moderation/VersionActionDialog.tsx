import { useEffect, useState } from "react";
import {
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
  TextField,
} from "@mui/material";
import { useTranslation } from "react-i18next";
import type { ModerationRequest, SetAction, SetStatus } from "@/models/api";
import { useSnackbar } from "@/app/SnackbarProvider";
import { ErrorState } from "@/shared/components/States";
import { setRef } from "@/shared/utils/format";
import { usePreview, useVersionAction } from "./api";
import { EffectText } from "./EffectText";
import { ReasonPicker } from "./ReasonPicker";

export interface VersionTarget {
  set_id: string;
  version: number;
  title: string;
  status: SetStatus;
}

interface VersionActionDialogProps {
  action: SetAction;
  target: VersionTarget | null;
  initialReason?: string;
  onClose: () => void;
}

const reasonMode: Record<SetAction, "required" | "optional" | "none"> = {
  approve: "none",
  reject: "required",
  hide: "optional",
  restore: "none",
};

const forceable = new Set(["author_banned", "set_withdrawn"]);

export function VersionActionDialog({ action, target, initialReason, onClose }: Readonly<VersionActionDialogProps>) {
  const { t } = useTranslation();
  const { notifyResult, notifyError } = useSnackbar();
  const mutation = useVersionAction();
  const [reason, setReason] = useState("");
  const [withdraw, setWithdraw] = useState(false);
  const [force, setForce] = useState(false);
  const [keepReports, setKeepReports] = useState(false);

  useEffect(() => {
    setReason(initialReason ?? "");
    setWithdraw(false);
    setForce(false);
    setKeepReports(false);
  }, [target, action, initialReason]);

  const request: ModerationRequest | null = target
    ? {
        action,
        reason: action === "reject" ? "preview" : "",
        items: [{ set_id: target.set_id, version: target.version }],
        force,
        withdraw,
        keep_reports: keepReports,
        partial: false,
        dry_run: true,
      }
    : null;
  const preview = usePreview(request);
  const item = preview.data?.items[0];
  const mode = reasonMode[action];
  const needsReason = mode === "required" && reason.trim() === "";
  const canForce = action === "approve" && item !== undefined && !item.ok && forceable.has(item.code ?? "");
  const blocked = mutation.isPending || needsReason || (item !== undefined && !item.ok);

  if (!target) return null;

  const confirm = async () => {
    try {
      const result = await mutation.mutateAsync({
        id: target.set_id,
        version: target.version,
        action,
        reason,
        force,
        withdraw,
        keepReports,
        expectStatus: target.status,
      });
      notifyResult(result);
      onClose();
    } catch (err) {
      notifyError(err);
    }
  };

  return (
    <Dialog open onClose={mutation.isPending ? undefined : onClose} fullWidth maxWidth="sm">
      <DialogTitle>{t(`moderation.${action}.title`, { ref: setRef(target.set_id, target.version), title: target.title })}</DialogTitle>
      <DialogContent sx={{ display: "flex", flexDirection: "column", gap: 2 }}>
        <DialogContentText>{t(`moderation.${action}.text`)}</DialogContentText>
        {preview.isLoading && (
          <Box sx={{ display: "flex", justifyContent: "center" }}>
            <CircularProgress size={20} />
          </Box>
        )}
        {preview.error && <ErrorState error={preview.error} onRetry={() => void preview.refetch()} />}
        {item && <EffectText item={item} action={action} />}
        {canForce && (
          <FormControlLabel
            control={<Checkbox checked={force} onChange={(e) => setForce(e.target.checked)} />}
            label={t(`moderation.force.${item.code ?? ""}`)}
          />
        )}
        {force && action === "approve" && (
          <FormControlLabel control={<Checkbox checked onChange={() => setForce(false)} />} label={t(`moderation.force.active`)} />
        )}
        {(action === "hide" || action === "reject") && (
          <FormControlLabel
            control={<Checkbox checked={withdraw} onChange={(e) => setWithdraw(e.target.checked)} />}
            label={t("moderation.withdrawToo")}
          />
        )}
        {(action === "hide" || action === "reject") && item?.ok && (item.reports > 0 || keepReports) && (
          <FormControlLabel
            control={<Checkbox checked={keepReports} onChange={(e) => setKeepReports(e.target.checked)} />}
            label={t("moderation.keepReports")}
          />
        )}
        {(action === "reject" || action === "hide") && <ReasonPicker scope={action} value={reason} onPick={setReason} />}
        {mode !== "none" && (
          <TextField
            autoFocus
            fullWidth
            size="small"
            label={t("app.reason")}
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            required={mode === "required"}
            helperText={needsReason ? t("app.reasonRequired") : t("moderation.reasonHint")}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !blocked) {
                e.preventDefault();
                void confirm();
              }
            }}
          />
        )}
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose} disabled={mutation.isPending} color="inherit">
          {t("app.cancel")}
        </Button>
        <Button
          autoFocus={mode === "none"}
          onClick={() => void confirm()}
          disabled={blocked}
          variant="contained"
          color={action === "approve" || action === "restore" ? "success" : "error"}
        >
          {t(`moderation.${action}.confirm`)}
        </Button>
      </DialogActions>
    </Dialog>
  );
}
