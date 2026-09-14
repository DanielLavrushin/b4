import { useEffect, useState } from "react";
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

export interface ReasonPrompt {
  title: string;
  text?: string;
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
    setBusy(true);
    try {
      await prompt.onConfirm(reason.trim());
    } catch {
      setBusy(false);
      return;
    }
    setBusy(false);
    onClose();
  };

  return (
    <Dialog open onClose={busy ? undefined : onClose} fullWidth maxWidth="sm">
      <DialogTitle>{prompt.title}</DialogTitle>
      <DialogContent sx={{ display: "flex", flexDirection: "column", gap: 2 }}>
        {prompt.text && <DialogContentText>{prompt.text}</DialogContentText>}
        {prompt.reason !== "none" && (
          <TextField
            autoFocus
            fullWidth
            size="small"
            label={t("app.reason")}
            value={reason}
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
