import { Alert, Collapse, Link } from "@mui/material";
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
