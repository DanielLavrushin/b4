import { useEffect, useState } from "react";
import { Alert, Button, Dialog, DialogActions, DialogContent, DialogTitle, TextField } from "@mui/material";
import { useTranslation } from "react-i18next";
import { useSnackbar } from "@/app/SnackbarProvider";
import type { EntryView } from "@/models/api";
import { setRef } from "@/shared/utils/format";
import { useSetText } from "./api";
import { revisionOf } from "./revision";

export function EditTextDialog({ entry, onClose }: Readonly<{ entry: EntryView | null; onClose: () => void }>) {
  const { t } = useTranslation();
  const save = useSetText();
  const { notifyResult, notifyError } = useSnackbar();
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [note, setNote] = useState("");

  useEffect(() => {
    if (!entry) return;
    setTitle(entry.title);
    setDescription(entry.description ?? "");
    setNote("");
  }, [entry]);

  if (!entry) return null;
  const changed = title.trim() !== entry.title || description.trim() !== (entry.description ?? "");
  const submit = async () => {
    try {
      notifyResult(
        await save.mutateAsync({
          id: entry.set_id,
          version: entry.version,
          body: { title, description, note, expect: revisionOf(entry) },
        }),
      );
      onClose();
    } catch (err) {
      notifyError(err);
    }
  };
  return (
    <Dialog open onClose={save.isPending ? undefined : onClose} fullWidth maxWidth="sm">
      <DialogTitle>{t("editText.title", { ref: setRef(entry.set_id, entry.version) })}</DialogTitle>
      <DialogContent sx={{ display: "flex", flexDirection: "column", gap: 2 }}>
        <Alert severity="info" variant="outlined">
          {t(entry.status === "active" ? "editText.introListed" : "editText.intro")}
        </Alert>
        <TextField autoFocus size="small" label={t("edit.setTitle")} value={title} onChange={(e) => setTitle(e.target.value)} required error={title.trim() === ""} />
        <TextField size="small" label={t("edit.description")} value={description} onChange={(e) => setDescription(e.target.value)} multiline minRows={3} />
        <TextField size="small" label={t("edit.note")} value={note} onChange={(e) => setNote(e.target.value)} helperText={t("edit.noteHint")} />
      </DialogContent>
      <DialogActions>
        <Button color="inherit" onClick={onClose} disabled={save.isPending}>
          {t("app.cancel")}
        </Button>
        <Button variant="contained" disabled={save.isPending || !changed || title.trim() === ""} onClick={() => void submit()}>
          {t("edit.save")}
        </Button>
      </DialogActions>
    </Dialog>
  );
}
