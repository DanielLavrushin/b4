import { Box, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";
import { colors } from "@design";
import type { EntryView } from "@/models/api";
import { ProjectionDiff } from "@/features/sets/ProjectionDiff";

export function EditedDiff({ entry, compact = false }: Readonly<{ entry: EntryView; compact?: boolean }>) {
  const { t } = useTranslation();
  const original = entry.original_projection;
  if (original === undefined) return null;
  const changes: { label: string; before: string; after: string }[] = [];
  if (entry.original_title !== undefined && entry.original_title !== entry.title) {
    changes.push({ label: t("edit.setTitle"), before: entry.original_title, after: entry.title });
  }
  if ((entry.original_description ?? "") !== (entry.description ?? "")) {
    changes.push({ label: t("edit.description"), before: entry.original_description ?? "", after: entry.description ?? "" });
  }
  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 1, minWidth: 0 }}>
      {changes.map((c) => (
        <Typography key={c.label} variant="body2" sx={{ overflowWrap: "anywhere" }}>
          {c.label}:{" "}
          <Typography component="span" variant="body2" sx={{ color: colors.text.secondary, textDecoration: "line-through" }}>
            {c.before || t("edit.emptyValue")}
          </Typography>{" "}
          {c.after || t("edit.emptyValue")}
        </Typography>
      ))}
      <ProjectionDiff
        before={original}
        after={entry.projection}
        beforeVersion={entry.version}
        title={t("edit.moderatorDiffTitle")}
        emptyText={t("edit.diffNone")}
        beforeLabel={t("edit.received")}
        afterLabel={t("edit.now")}
        compact={compact}
      />
    </Box>
  );
}
