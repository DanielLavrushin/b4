import { useState } from "react";
import { Box, Button, Tooltip, Typography } from "@mui/material";
import { CategoryIcon, ClearIcon, InfoIcon } from "@b4.icons";
import { B4ChipList } from "@b4.elements";
import SettingAutocomplete from "@common/B4Autocomplete";
import { useTranslation } from "react-i18next";

interface GeoCategoryPanelProps {
  title: string;
  tooltip: string;
  inputLabel: string;
  inputPlaceholder: string;
  helperText: string;
  activeTitle: string;
  options: string[];
  loading: boolean;
  selected: string[];
  breakdown?: Record<string, number>;
  onSelectedChange: (categories: string[]) => void;
  onPreview?: (category: string) => void;
}

const CategoryLabel = ({
  category,
  count,
}: {
  category: string;
  count?: number;
}) => (
  <Box sx={{ display: "flex", alignItems: "center", gap: 0.5 }}>
    <span>{category}</span>
    {count && (
      <Typography
        component="span"
        variant="caption"
        sx={{
          bgcolor: "action.selected",
          px: 0.5,
          borderRadius: 1,
          fontWeight: 600,
        }}
      >
        {count}
      </Typography>
    )}
  </Box>
);

export const GeoCategoryPanel = ({
  title,
  tooltip,
  inputLabel,
  inputPlaceholder,
  helperText,
  activeTitle,
  options,
  loading,
  selected,
  breakdown,
  onSelectedChange,
  onPreview,
}: GeoCategoryPanelProps) => {
  const { t } = useTranslation();
  const [input, setInput] = useState("");

  const handleAdd = (category: string) => {
    if (category && !selected.includes(category)) {
      onSelectedChange([...selected, category]);
      setInput("");
    }
  };

  return (
    <Box sx={{ mb: 2 }}>
      <Typography
        variant="h6"
        sx={{ display: "flex", alignItems: "center", gap: 1, mb: 2 }}
      >
        <CategoryIcon /> {title}
        <Tooltip title={tooltip}>
          <InfoIcon fontSize="small" color="action" />
        </Tooltip>
      </Typography>

      <SettingAutocomplete
        label={inputLabel}
        value={input}
        options={options}
        onChange={setInput}
        onSelect={handleAdd}
        loading={loading}
        placeholder={inputPlaceholder}
        helperText={helperText}
      />

      <Box sx={{ mt: 2 }}>
        {selected.length > 0 && (
          <Box
            sx={{
              display: "flex",
              justifyContent: "space-between",
              alignItems: "center",
              mb: 1,
            }}
          >
            <Typography variant="subtitle2">{activeTitle}</Typography>
            <Button
              size="small"
              onClick={() => onSelectedChange([])}
              startIcon={<ClearIcon />}
            >
              {t("core.clearAll")}
            </Button>
          </Box>
        )}
        <B4ChipList
          items={selected}
          getKey={(c) => c}
          getLabel={(c) => (
            <CategoryLabel category={c} count={breakdown?.[c]} />
          )}
          onDelete={(c) => onSelectedChange(selected.filter((s) => s !== c))}
          onClick={onPreview}
        />
      </Box>
    </Box>
  );
};
