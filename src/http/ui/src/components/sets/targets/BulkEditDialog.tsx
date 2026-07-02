import { useState, useEffect } from "react";
import { Box, Button } from "@mui/material";
import { B4Dialog, B4TextField } from "@b4.elements";
import { EditIcon } from "@b4.icons";
import { useTranslation } from "react-i18next";

interface BulkEditDialogProps {
  open: boolean;
  items: string[];
  onClose: () => void;
  onSave: (items: string[]) => void;
}

export const BulkEditDialog = ({
  open,
  items,
  onClose,
  onSave,
}: BulkEditDialogProps) => {
  const { t } = useTranslation();
  const [text, setText] = useState("");

  useEffect(() => {
    if (open) setText(items.join("\n"));
  }, [open, items]);

  const handleSave = () => {
    const lines = text
      .split(/[\n\r]+/)
      .map((l) => l.trim())
      .filter(Boolean);
    onSave([...new Set(lines)]);
    onClose();
  };

  return (
    <B4Dialog
      title={t("sets.targets.bulkEdit")}
      subtitle={t("sets.targets.bulkEditSubtitle")}
      icon={<EditIcon />}
      open={open}
      onClose={onClose}
      maxWidth="sm"
      fullWidth
      actions={
        <Box sx={{ display: "flex", gap: 1 }}>
          <Button onClick={onClose}>{t("core.cancel")}</Button>
          <Button variant="contained" onClick={handleSave}>
            {t("core.save")}
          </Button>
        </Box>
      }
    >
      <B4TextField
        multiline
        minRows={10}
        maxRows={25}
        value={text}
        onChange={(e) => setText(e.target.value)}
        placeholder={t("sets.targets.bulkEditPlaceholder")}
        helperText={t("sets.targets.bulkEditHelper", {
          count: text.split(/[\n\r]+/).filter((l) => l.trim()).length,
        })}
        fullWidth
      />
    </B4Dialog>
  );
};
