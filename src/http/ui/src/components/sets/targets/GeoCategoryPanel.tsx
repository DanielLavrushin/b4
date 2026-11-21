import { useEffect, useMemo, useState } from "react";
import { Box, Button, Tooltip, Typography } from "@mui/material";
import { CategoryIcon, ClearIcon } from "@b4.icons";
import { B4ChipList, B4Help } from "@b4.elements";
import SettingAutocomplete from "@common/B4Autocomplete";
import { useTranslation } from "react-i18next";
import { useChangedFields } from "@context/ChangedFieldsContext";

interface GeoCategoryPanelProps {
  title: string;
  description?: string;
  info?: React.ReactNode;
  inputLabel: string;
  inputPlaceholder: string;
  helperText: string;
  activeTitle: string;
  options: string[];
  loading: boolean;
  selected: string[];
  breakdown?: Record<string, number>;
  missingText?: string;
  stripAttrs?: boolean;
  onSelectedChange: (categories: string[]) => void;
  onPreview?: (category: string) => void;
  path?: string;
}

const tagOf = (category: string, stripAttrs: boolean) =>
  (stripAttrs ? category.split("@")[0] : category).toLowerCase();

const CategoryLabel = ({
  category,
  count,
}: {
  category: string;
  count?: number;
}) => (
  <Box sx={{ display: "flex", alignItems: "center", gap: 0.5 }}>
    <span>{category}</span>
    {count ? (
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
    ) : null}
  </Box>
);

export const GeoCategoryPanel = ({
  title,
  description,
  info,
  inputLabel,
  inputPlaceholder,
  helperText,
  activeTitle,
  options,
  loading,
  selected,
  breakdown,
  missingText,
  stripAttrs = false,
  onSelectedChange,
  onPreview,
  path,
}: GeoCategoryPanelProps) => {
  const { t } = useTranslation();
  const [input, setInput] = useState("");
  const isMissing = (category: string): boolean =>
    !!missingText &&
    !loading &&
    options.length > 0 &&
    !options.includes(tagOf(category, stripAttrs));
  const { changedValues, registerLabel } = useChangedFields();
  const leaf = path ? changedValues.get(path) : undefined;
  const leafSets = useMemo(() => {
    if (!leaf || !Array.isArray(leaf.after) || !Array.isArray(leaf.before)) {
      return null;
    }
    return {
      after: new Set(leaf.after as unknown[]),
      before: new Set(leaf.before as unknown[]),
    };
  }, [leaf]);
  const has = (category: string): boolean =>
    !!leafSets && leafSets.after.has(category) !== leafSets.before.has(category);
  useEffect(() => {
    if (!path) return;
    registerLabel(path, title);
  }, [path, title, registerLabel]);

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
        sx={{
          display: "flex",
          alignItems: "center",
          gap: 1,
          mb: description ? 0.5 : 2,
        }}
      >
        <CategoryIcon /> {title}
        <B4Help label={title} info={info} />
      </Typography>
      {description && (
        <Typography
          variant="caption"
          sx={{ display: "block", color: "text.secondary", mb: 2 }}
        >
          {description}
        </Typography>
      )}

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
          getLabel={(c) =>
            isMissing(c) ? (
              <Tooltip title={missingText} describeChild>
                <Box component="span" sx={{ display: "inline-flex" }}>
                  <CategoryLabel category={c} count={breakdown?.[c]} />
                </Box>
              </Tooltip>
            ) : (
              <CategoryLabel category={c} count={breakdown?.[c]} />
            )
          }
          onDelete={(c) => onSelectedChange(selected.filter((s) => s !== c))}
          onClick={onPreview}
          highlight={path ? (c) => has(c) : undefined}
          error={isMissing}
        />
      </Box>
    </Box>
  );
};
