import { useState, useEffect } from "react";
import { Box, Button, List, ListItem, ListItemText, Skeleton } from "@mui/material";
import { B4Alert, B4Dialog, B4ModalAlertStrip } from "@b4.elements";
import { CategoryIcon, InfoIcon } from "@b4.icons";
import { useTranslation } from "react-i18next";

interface CategoryPreview {
  category: string;
  total_domains: number;
  preview_count: number;
  preview: string[];
}

interface CategoryPreviewDialogProps {
  category: string | null;
  onClose: () => void;
}

export const CategoryPreviewDialog = ({
  category,
  onClose,
}: CategoryPreviewDialogProps) => {
  const { t } = useTranslation();
  const [data, setData] = useState<CategoryPreview | undefined>(undefined);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!category) return;
    let cancelled = false;
    const load = async () => {
      setData(undefined);
      setLoading(true);
      try {
        const response = await fetch(
          `/api/geosite/category?tag=${encodeURIComponent(category)}`,
        );
        if (response.ok) {
          const preview = (await response.json()) as CategoryPreview;
          if (!cancelled) setData(preview);
        }
      } catch (error) {
        console.error("Failed to preview category:", error);
      } finally {
        if (!cancelled) setLoading(false);
      }
    };
    void load();
    return () => {
      cancelled = true;
    };
  }, [category]);

  const renderContent = () => {
    if (loading) {
      return (
        <Box sx={{ p: 2 }}>
          <Skeleton variant="text" />
          <Skeleton variant="text" />
          <Skeleton variant="text" />
        </Box>
      );
    }
    if (data) {
      const { total_domains, preview_count, preview } = data;
      const showingMore = total_domains > preview_count;
      return (
        <>
          <B4ModalAlertStrip tone="primary" icon={<InfoIcon />} sx={{ mb: 2 }}>
            {t("sets.targets.previewTotal", { count: total_domains })}
            {showingMore &&
              ` (${t("sets.targets.previewShowing", { count: preview_count })})`}
          </B4ModalAlertStrip>
          <List dense sx={{ maxHeight: 600, overflow: "auto" }}>
            {preview.map((domain) => (
              <ListItem key={domain}>
                <ListItemText primary={domain} />
              </ListItem>
            ))}
          </List>
        </>
      );
    }
    return (
      <B4Alert severity="error">{t("sets.targets.previewFailed")}</B4Alert>
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
      {renderContent()}
    </B4Dialog>
  );
};
