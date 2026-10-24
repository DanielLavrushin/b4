import { useState } from "react";
import { Box, IconButton, TextField } from "@mui/material";
import CheckIcon from "@mui/icons-material/Check";
import CloseIcon from "@mui/icons-material/Close";

interface B4InlineEditProps {
  value: string;
  onSave: (value: string) => Promise<void> | void;
  onCancel: () => void;
  disabled?: boolean;
  allowEmpty?: boolean;
  width?: number;
}

export const B4InlineEdit = ({
  value: initialValue,
  onSave,
  onCancel,
  disabled = false,
  allowEmpty = false,
  width = 150,
}: B4InlineEditProps) => {
  const [value, setValue] = useState(initialValue);
  const [saving, setSaving] = useState(false);
  const blocked = !allowEmpty && !value.trim();

  const handleSave = async () => {
    if (blocked) return;
    setSaving(true);
    try {
      await onSave(value.trim());
    } finally {
      setSaving(false);
    }
  };

  return (
    <Box sx={{ display: "flex", alignItems: "center", gap: 0.5 }}>
      <TextField
        size="small"
        value={value}
        onChange={(e) => setValue(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter") void handleSave();
          else if (e.key === "Escape") onCancel();
        }}
        autoFocus
        disabled={saving || disabled}
        sx={{
          width,
          "& .MuiInputBase-input": { py: 0.5, fontSize: "0.85rem" },
        }}
      />
      <IconButton
        size="small"
        onClick={() => void handleSave()}
        disabled={saving || blocked}
        color="success"
      >
        <CheckIcon fontSize="small" />
      </IconButton>
      <IconButton size="small" onClick={onCancel} disabled={saving}>
        <CloseIcon fontSize="small" />
      </IconButton>
    </Box>
  );
};
