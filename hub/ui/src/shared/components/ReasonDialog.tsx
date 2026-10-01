import { useEffect, useRef, useState, type ReactNode } from "react";
import {
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
  TextField,
} from "@mui/material";
import { useTranslation } from "react-i18next";
import type { ReasonScope } from "@/models/api";
import { ReasonPicker } from "@/features/moderation/ReasonPicker";

export interface ReasonPrompt {
  title: string;
  scope?: ReasonScope;
  text?: string;
  details?: ReactNode;
  reasonLabel?: string;
  confirmLabel: string;
  reason: "required" | "optional" | "none";
  destructive?: boolean;
  confirmText?: string;
  onConfirm: (reason: string) => Promise<void> | void;
}

interface ReasonDialogProps {
  prompt: ReasonPrompt | null;
  onClose: () => void;
}

export function ReasonDialog({ prompt, onClose }: ReasonDialogProps) {
  const { t } = useTranslation();
  const [reason, setReason] = useState("");
  const [typed, setTyped] = useState("");
  const [busy, setBusy] = useState(false);
  const inFlight = useRef(false);

  useEffect(() => {
    if (prompt) {
      setReason("");
      setTyped("");
      setBusy(false);
    }
  }, [prompt]);

  if (!prompt) return null;
  const needsReason = prompt.reason === "required" && reason.trim() === "";
  const needsTyping = prompt.confirmText !== undefined && typed.trim() !== prompt.confirmText;
  const blocked = needsReason || needsTyping;

  const confirm = async () => {
    if (inFlight.current) return;
    inFlight.current = true;
    setBusy(true);
    try {
      await prompt.onConfirm(reason.trim());
    } catch {
      inFlight.current = false;
      setBusy(false);
      return;
    }
    inFlight.current = false;
    setBusy(false);
    onClose();
  };

  return (
    <Dialog open onClose={busy ? undefined : onClose} fullWidth maxWidth="sm">
      <DialogTitle>{prompt.title}</DialogTitle>
      <DialogContent sx={{ display: "flex", flexDirection: "column", gap: 2 }}>
        {prompt.text && <DialogContentText>{prompt.text}</DialogContentText>}
        {prompt.details}
        {prompt.scope && prompt.reason !== "none" && <ReasonPicker scope={prompt.scope} value={reason} onPick={setReason} />}
        {prompt.reason !== "none" && (
          <TextField
            autoFocus
            fullWidth
            size="small"
            label={prompt.reasonLabel ?? t("app.reason")}
            value={reason}
            disabled={busy}
            onChange={(e) => setReason(e.target.value)}
            required={prompt.reason === "required"}
            helperText={needsReason ? t("app.reasonRequired") : " "}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !blocked) {
                e.preventDefault();
                void confirm();
              }
            }}
          />
        )}
        {prompt.confirmText !== undefined && (
          <TextField
            autoFocus={prompt.reason === "none"}
            fullWidth
            size="small"
            label={t("app.typeToConfirm", { text: prompt.confirmText })}
            value={typed}
            disabled={busy}
            onChange={(e) => setTyped(e.target.value)}
            slotProps={{ input: { sx: { fontFamily: "monospace" } } }}
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
        <Button onClick={onClose} disabled={busy} color="inherit">
          {t("app.cancel")}
        </Button>
        <Button
          autoFocus={prompt.reason === "none" && prompt.confirmText === undefined}
          onClick={() => void confirm()}
          disabled={busy || blocked}
          variant="contained"
          color={prompt.destructive ? "error" : "primary"}
        >
          {prompt.confirmLabel}
        </Button>
      </DialogActions>
    </Dialog>
  );
}
