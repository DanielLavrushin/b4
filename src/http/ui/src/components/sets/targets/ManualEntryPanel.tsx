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
  notice?: string;
  normalize?: (raw: string) => string[];
  validate?: (entry: string) => string | null;
  highlight?: (item: string) => boolean;
  getItemLabel?: (item: string) => ReactNode;
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
  notice,
  normalize,
  validate,
  highlight,
  getItemLabel,
  onItemsChange,
  onInputChange,
}: ManualEntryPanelProps) => {
  const { t } = useTranslation();
  const [input, setInput] = useState("");
  const [invalid, setInvalid] = useState("");
  const [bulkOpen, setBulkOpen] = useState(false);

  const handleInput = (value: string) => {
    setInput(value);
    setInvalid("");
    onInputChange?.(value);
  };

  const handleAdd = (): boolean => {
    const value = input.trim();
    if (!value) return true;

    const existing = new Set(items);
    const next = [...items];
    const rejected: string[] = [];
    let problem = "";
    for (const raw of value.split(/[\s,|]+/).filter(Boolean)) {
      const expanded = normalize ? normalize(raw) : [raw.trim()];
      const error = validate
        ? expanded.map(validate).find((message) => message)
        : undefined;
      if (error) {
        rejected.push(raw);
        problem ||= error;
        continue;
      }
      for (const item of expanded) {
        if (item && !existing.has(item)) {
          existing.add(item);
          next.push(item);
        }
      }
    }

    onItemsChange(next);
    handleInput(rejected.join(" "));
    setInvalid(problem);
    return rejected.length === 0;
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
            if (e.key === "Tab" && !input.trim()) return;
            if (e.key === "Enter" || e.key === "Tab" || e.key === ",") {
              if (handleAdd() || e.key !== "Tab") e.preventDefault();
            }
          }}
          error={!!invalid}
          helperText={invalid || inputHelper}
          placeholder={inputPlaceholder}
        />
        <B4PlusButton onClick={handleAdd} disabled={!input.trim()} />
      </Box>
      {notice && (
        <B4Alert severity="info" sx={{ mt: 1 }}>
          {notice}
        </B4Alert>
      )}
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
          getLabel={(item) => getItemLabel?.(item) ?? item}
          highlight={highlight}
          onDelete={(item) => onItemsChange(items.filter((i) => i !== item))}
          emptyMessage={emptyMessage}
          showEmpty
          collapsedMax={20}
        />
      </Box>
      <BulkEditDialog
        open={bulkOpen}
        items={items}
        validate={validate}
        onClose={() => setBulkOpen(false)}
        onSave={onItemsChange}
      />
    </Box>
  );
};
