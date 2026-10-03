import { Box, Button, Collapse, Typography } from "@mui/material";
import EditIcon from "@mui/icons-material/Edit";
import { useId, useState } from "react";
import { useTranslation } from "react-i18next";
import { colors } from "@design";
import type { EntryView } from "@/models/api";
import { formatAgo, formatStamp } from "@/shared/utils/format";
import { EditedDiff } from "./card/EditedDiff";

export function EditedNotice({ entry }: Readonly<{ entry: EntryView }>) {
  const { t } = useTranslation();
  const [compare, setCompare] = useState(false);
  const diffId = useId();
  if (!entry.edited_at) return null;
  const comparable = entry.original_projection !== undefined;
  const text = [
    `${t("queue.edited", { when: formatAgo(t, entry.edited_at) })}, ${formatStamp(entry.edited_at)}`,
    entry.edit_note ? t("queue.editedNote", { note: entry.edit_note }) : "",
  ]
    .filter(Boolean)
    .join(" · ");
  return (
    <Box sx={{ minWidth: 0 }}>
      <Box sx={{ display: "flex", alignItems: "center", flexWrap: "wrap", columnGap: 1, rowGap: 0.25, color: colors.text.secondary }}>
        <EditIcon aria-hidden sx={{ fontSize: 14 }} />
        <Typography variant="caption" sx={{ minWidth: 0, overflowWrap: "anywhere" }}>
          {text}
        </Typography>
        {comparable && (
          <Button
            size="small"
            aria-expanded={compare}
            aria-controls={compare ? diffId : undefined}
            onClick={() => setCompare((v) => !v)}
            sx={{ py: 0, minWidth: 0, color: colors.text.secondary, "&:hover": { color: colors.text.primary } }}
          >
            {compare ? t("queue.hideCompareOriginal") : t("queue.compareOriginal")}
          </Button>
        )}
      </Box>
      {comparable && (
        <Collapse in={compare} unmountOnExit>
          <Box id={diffId} sx={{ mt: 1, p: 1.5, bgcolor: colors.background.dark, border: `1px solid ${colors.border.light}`, borderRadius: 1.5 }}>
            <EditedDiff entry={entry} />
          </Box>
        </Collapse>
      )}
    </Box>
  );
}
