import { useState, ReactNode } from "react";
import { Box, Button, Tooltip, Typography } from "@mui/material";
import { ClearIcon, EditIcon, InfoIcon } from "@b4.icons";
import { B4Alert, B4ChipList, B4PlusButton, B4TextField } from "@b4.elements";
import { useTranslation } from "react-i18next";
import { BulkEditDialog } from "./BulkEditDialog";

interface ManualEntryPanelProps {
  icon: ReactNode;
  title: string;
  tooltip: string;
  inputLabel: string;
  inputHelper: string;
  inputPlaceholder: string;
  activeTitle: string;
  emptyMessage: string;
  items: string[];
  warning?: string;
  onItemsChange: (items: string[]) => void;
  onInputChange?: (value: string) => void;
}

export const ManualEntryPanel = ({
  icon,
  title,
  tooltip,
  inputLabel,
  inputHelper,
  inputPlaceholder,
  activeTitle,
  emptyMessage,
  items,
  warning,
  onItemsChange,
  onInputChange,
}: ManualEntryPanelProps) => {
  const { t } = useTranslation();
  const [input, setInput] = useState("");
  const [bulkOpen, setBulkOpen] = useState(false);

  const handleInput = (value: string) => {
    setInput(value);
    onInputChange?.(value);
  };

  const handleAdd = () => {
    const value = input.trim();
    if (!value) return;

    const existing = new Set(items);
    const next = [...items];
    for (const raw of value.split(/[\s,|]+/).filter(Boolean)) {
      const item = raw.trim();
      if (item && !existing.has(item)) {
        existing.add(item);
        next.push(item);
      }
    }

    onItemsChange(next);
    handleInput("");
  };

  return (
    <Box>
      <Typography
        variant="h6"
        sx={{ display: "flex", alignItems: "center", gap: 1, mb: 2 }}
      >
        {icon} {title}
        <Tooltip title={tooltip}>
          <InfoIcon fontSize="small" color="action" />
        </Tooltip>
      </Typography>
      <Box sx={{ display: "flex", gap: 1, alignItems: "flex-start" }}>
        <B4TextField
          label={inputLabel}
          value={input}
          onChange={(e) => handleInput(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" || e.key === "Tab" || e.key === ",") {
              e.preventDefault();
              handleAdd();
            }
          }}
          helperText={inputHelper}
          placeholder={inputPlaceholder}
        />
        <B4PlusButton onClick={handleAdd} disabled={!input.trim()} />
      </Box>
      {warning && (
        <B4Alert severity="warning" sx={{ mt: 1 }}>
          {t("sets.targets.duplicateWarning")} {warning}
        </B4Alert>
      )}
      <Box sx={{ mt: 2 }}>
        {items.length > 0 && (
          <Box
            sx={{
              display: "flex",
              justifyContent: "space-between",
              alignItems: "center",
              mb: 1,
            }}
          >
            <Typography variant="subtitle2">{activeTitle}</Typography>
            <Box sx={{ display: "flex", gap: 1 }}>
              <Button
                size="small"
                onClick={() => setBulkOpen(true)}
                startIcon={<EditIcon />}
              >
                {t("sets.targets.bulkEdit")}
              </Button>
              <Button
                size="small"
                onClick={() => onItemsChange([])}
                startIcon={<ClearIcon />}
              >
                {t("core.clearAll")}
              </Button>
            </Box>
          </Box>
        )}
        <B4ChipList
          items={items}
          getKey={(item) => item}
          getLabel={(item) => item}
          onDelete={(item) => onItemsChange(items.filter((i) => i !== item))}
          emptyMessage={emptyMessage}
          showEmpty
          collapsedMax={20}
        />
      </Box>
      <BulkEditDialog
        open={bulkOpen}
        items={items}
        onClose={() => setBulkOpen(false)}
        onSave={onItemsChange}
      />
    </Box>
  );
};
