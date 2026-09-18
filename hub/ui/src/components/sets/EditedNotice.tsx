import { Alert, Box, Collapse, Link, Typography } from "@mui/material";
import { colors } from "@design";
import EditOutlinedIcon from "@mui/icons-material/EditOutlined";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import type { EntryView } from "@/models/api";
import { formatAgo, formatStamp } from "@/utils/format";
import { ProjectionDiff } from "./ProjectionDiff";

export function EditedNotice({ entry }: { entry: EntryView }) {
  const { t } = useTranslation();
  const [compare, setCompare] = useState(false);
  if (!entry.edited_at) return null;
  const original = entry.original_projection;
  const textChanges: { label: string; before: string; after: string }[] = [];
  if (entry.original_title !== undefined && entry.original_title !== entry.title) {
    textChanges.push({ label: t("edit.setTitle"), before: entry.original_title, after: entry.title });
  }
  if ((entry.original_description ?? "") !== (entry.description ?? "") && entry.original_projection !== undefined) {
    textChanges.push({ label: t("edit.description"), before: entry.original_description ?? "", after: entry.description ?? "" });
  }
  const note = entry.edit_note ? ` · ${t("queue.editedNote", { note: entry.edit_note })}` : "";
  return (
    <>
      <Alert
        severity="info"
        variant="outlined"
        icon={<EditOutlinedIcon fontSize="inherit" />}
        title={formatStamp(entry.edited_at)}
        action={
          original !== undefined ? (
            <Link component="button" type="button" underline="hover" variant="body2" onClick={() => setCompare((v) => !v)}>
              {compare ? t("queue.hideCompareOriginal") : t("queue.compareOriginal")}
            </Link>
          ) : undefined
        }
      >
        {t("queue.edited", { when: formatAgo(t, entry.edited_at) })}
        {note}
      </Alert>
      {original !== undefined && (
        <Collapse in={compare} unmountOnExit>
          {textChanges.length > 0 && (
            <Box sx={{ mb: 1.5, display: "flex", flexDirection: "column", gap: 0.5 }}>
              {textChanges.map((c) => (
                <Typography key={c.label} variant="body2">
                  {c.label}:{" "}
                  <Typography component="span" variant="body2" sx={{ color: colors.text.secondary, textDecoration: "line-through" }}>
                    {c.before || t("edit.emptyValue")}
                  </Typography>{" "}
                  {c.after || t("edit.emptyValue")}
                </Typography>
              ))}
            </Box>
          )}
          <ProjectionDiff
            before={original}
            after={entry.projection}
            beforeVersion={entry.version}
            title={t("edit.moderatorDiffTitle")}
            emptyText={t("edit.diffNone")}
            beforeLabel={t("edit.received")}
            afterLabel={t("edit.now")}
          />
        </Collapse>
      )}
    </>
  );
}
