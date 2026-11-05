import { useEffect, useMemo, useState } from "react";
import { Box, Button, Tooltip, Typography } from "@mui/material";
import { CategoryIcon, ClearIcon, InfoIcon } from "@b4.icons";
import { B4Alert, B4ChipList } from "@b4.elements";
import SettingAutocomplete from "@common/B4Autocomplete";
import { useTranslation } from "react-i18next";
import { useChangedFields } from "@context/ChangedFieldsContext";

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
  excluded?: Record<string, string[]>;
  otherSets?: (category: string) => string[];
  path?: string;
}

export const EDITED_SUFFIX = "_edited";

const CategoryLabel = ({
  category,
  count,
  edited,
}: {
  category: string;
  count?: number;
  edited?: boolean;
}) => (
  <Box sx={{ display: "flex", alignItems: "center", gap: 0.5 }}>
    <span>{edited ? `${category}${EDITED_SUFFIX}` : category}</span>
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
  excluded,
  otherSets,
  path,
}: GeoCategoryPanelProps) => {
  const { t } = useTranslation();
  const [input, setInput] = useState("");
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

  const sharedCategories = otherSets
    ? selected.flatMap((category) => {
        const sets = otherSets(category);
        return sets.length > 0 ? [{ category, sets }] : [];
      })
    : [];

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
          getLabel={(c) => {
            const removed = excluded?.[c] ?? [];
            const count = breakdown?.[c];
            const label = (
              <CategoryLabel
                category={c}
                count={count ? Math.max(count - removed.length, 0) : count}
                edited={removed.length > 0}
              />
            );
            return removed.length > 0 ? (
              <Tooltip
                title={t("sets.targets.categoryEditedTooltip", {
                  count: removed.length,
                  entries: removed.join(", "),
                })}
              >
                {label}
              </Tooltip>
            ) : (
              label
            );
          }}
          onDelete={(c) => onSelectedChange(selected.filter((s) => s !== c))}
          onClick={onPreview}
          highlight={path ? (c) => has(c) : undefined}
        />
        {sharedCategories.length > 0 && (
          <B4Alert severity="warning" sx={{ mt: 2 }}>
            {sharedCategories
              .map(({ category, sets }) =>
                t("sets.targets.categoryInOtherSets", {
                  category,
                  sets: sets.join(", "),
                }),
              )
              .join(" ")}{" "}
            {t("sets.targets.categoryOverlapRule")}
          </B4Alert>
        )}
      </Box>
    </Box>
  );
};
