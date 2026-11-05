import { useState, useEffect } from "react";
import {
  Box,
  Button,
  IconButton,
  InputAdornment,
  List,
  ListItem,
  ListItemText,
  Skeleton,
  Tooltip,
  Typography,
} from "@mui/material";
import {
  B4Alert,
  B4Dialog,
  B4ModalAlertStrip,
  B4TextField,
} from "@b4.elements";
import {
  BlockIcon,
  CategoryIcon,
  InfoIcon,
  RestoreIcon,
  SearchIcon,
} from "@b4.icons";
import { useTranslation } from "react-i18next";

const PREVIEW_LIMIT = 500;

interface CategoryPreview {
  category: string;
  total_domains: number;
  matched: number;
  preview_count: number;
  preview: string[];
}

export type GeoKind = "geosite" | "geoip";

interface CategoryPreviewDialogProps {
  kind: GeoKind;
  category: string | null;
  excluded?: string[];
  onExcludedChange?: (entries: string[]) => void;
  onClose: () => void;
}

export const CategoryPreviewDialog = ({
  kind,
  category,
  excluded = [],
  onExcludedChange,
  onClose,
}: CategoryPreviewDialogProps) => {
  const { t } = useTranslation();
  const [data, setData] = useState<CategoryPreview | undefined>(undefined);
  const [loading, setLoading] = useState(false);
  const [search, setSearch] = useState("");
  const [query, setQuery] = useState("");

  useEffect(() => {
    setSearch("");
    setQuery("");
    setData(undefined);
  }, [category]);

  useEffect(() => {
    const timer = setTimeout(() => setQuery(search.trim()), 300);
    return () => clearTimeout(timer);
  }, [search]);

  useEffect(() => {
    if (!category) return;
    let cancelled = false;
    const load = async () => {
      setLoading(true);
      try {
        const params = new URLSearchParams({
          tag: category,
          limit: String(PREVIEW_LIMIT),
        });
        if (query) params.set("q", query);
        const response = await fetch(`/api/${kind}/category?${params}`);
        const preview = response.ok
          ? ((await response.json()) as CategoryPreview)
          : undefined;
        if (!cancelled) setData(preview);
      } catch (error) {
        console.error("Failed to preview category:", error);
        if (!cancelled) setData(undefined);
      } finally {
        if (!cancelled) setLoading(false);
      }
    };
    void load();
    return () => {
      cancelled = true;
    };
  }, [kind, category, query]);

  const excludedSet = new Set(excluded.map((e) => e.toLowerCase()));
  const isExcluded = (entry: string) => excludedSet.has(entry.toLowerCase());
  const toggle = (entry: string) => {
    if (!onExcludedChange) return;
    onExcludedChange(
      isExcluded(entry)
        ? excluded.filter((e) => e.toLowerCase() !== entry.toLowerCase())
        : [...excluded, entry],
    );
  };

  const entryRow = (entry: string) => {
    const removed = isExcluded(entry);
    return (
      <ListItem
        key={entry}
        secondaryAction={
          onExcludedChange && (
            <Tooltip
              title={t(
                removed
                  ? "sets.targets.previewRestore"
                  : "sets.targets.previewRemove",
              )}
            >
              <IconButton edge="end" size="small" onClick={() => toggle(entry)}>
                {removed ? (
                  <RestoreIcon fontSize="small" />
                ) : (
                  <BlockIcon fontSize="small" />
                )}
              </IconButton>
            </Tooltip>
          )
        }
      >
        <ListItemText
          primary={entry}
          slotProps={{
            primary: {
              sx: removed
                ? { textDecoration: "line-through", color: "text.disabled" }
                : undefined,
            },
          }}
        />
      </ListItem>
    );
  };

  const renderList = () => {
    if (loading && !data) {
      return (
        <Box sx={{ p: 2 }}>
          <Skeleton variant="text" />
          <Skeleton variant="text" />
          <Skeleton variant="text" />
        </Box>
      );
    }
    if (!data) {
      return (
        <B4Alert severity="error">{t("sets.targets.previewFailed")}</B4Alert>
      );
    }
    return (
      <List dense sx={{ maxHeight: 480, overflow: "auto" }}>
        {data.preview.map(entryRow)}
      </List>
    );
  };

  return (
    <B4Dialog
      title={(category ?? "").toUpperCase()}
      subtitle={t("sets.targets.previewSubtitle")}
      icon={<CategoryIcon />}
      open={!!category}
      onClose={onClose}
      actions={
        <Button variant="contained" onClick={onClose}>
          {t("core.close")}
        </Button>
      }
    >
      {data && (
        <B4ModalAlertStrip tone="primary" icon={<InfoIcon />} sx={{ mb: 2 }}>
          {t(
            kind === "geoip"
              ? "sets.targets.previewTotalIps"
              : "sets.targets.previewTotal",
            { count: data.total_domains },
          )}
          {query && ` (${t("sets.targets.previewMatched", { count: data.matched })})`}
          {data.matched > data.preview_count &&
            ` (${t("sets.targets.previewShowing", { count: data.preview_count })})`}
        </B4ModalAlertStrip>
      )}

      {onExcludedChange && (
        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
          {t("sets.targets.previewExcludeHint")}
        </Typography>
      )}

      {excluded.length > 0 && (
        <Box sx={{ mb: 2 }}>
          <Typography variant="subtitle2">
            {t("sets.targets.previewExcluded", { count: excluded.length })}
          </Typography>
          <List dense>{excluded.map(entryRow)}</List>
        </Box>
      )}

      <B4TextField
        label={t("sets.targets.previewSearch")}
        value={search}
        onChange={(e) => setSearch(e.target.value)}
        slotProps={{
          input: {
            startAdornment: (
              <InputAdornment position="start">
                <SearchIcon fontSize="small" />
              </InputAdornment>
            ),
          },
        }}
      />

      {renderList()}
    </B4Dialog>
  );
};
