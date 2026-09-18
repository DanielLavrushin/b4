import { Alert, Box, Button, Chip, Collapse, Divider, Link, Paper, Stack, Typography } from "@mui/material";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { colors, radiusPx } from "@design";
import type { EntryView } from "@/models/api";
import { useSetDetail } from "@/api/hub";
import { formatAgo, formatStamp, setRef } from "@/utils/format";
import { EditedNotice } from "./EditedNotice";
import { EntryFacts, Origin } from "./EntryFacts";
import { ProjectionDiff } from "./ProjectionDiff";
import type { Moderation } from "./useModeration";

interface QueueCardProps {
  entry: EntryView;
  moderation: Moderation;
}

function Comparison({ entry }: { entry: EntryView }) {
  const detail = useSetDetail(entry.set_id);
  const current = entry.lineage?.current_version;
  const listed = detail.data?.versions.find((v) => v.version === current);
  if (!listed || current === undefined) return null;
  return <ProjectionDiff before={listed.projection} after={entry.projection} beforeVersion={current} />;
}

export function QueueCard({ entry, moderation }: QueueCardProps) {
  const { t } = useTranslation();
  const [compare, setCompare] = useState(false);
  const lineage = entry.lineage;
  const canCompare = lineage?.kind === "replaces" && lineage.current_version !== undefined;

  return (
    <Paper
      variant="outlined"
      sx={{
        p: "20px 24px",
        bgcolor: colors.background.paper,
        border: `1px solid ${colors.border.default}`,
        borderRadius: `${radiusPx.md}px`,
        display: "flex",
        flexDirection: "column",
        gap: 1.5,
        minWidth: 0,
      }}
    >
      <Box sx={{ display: "flex", alignItems: "flex-start", gap: 2, flexWrap: "wrap" }}>
        <Box sx={{ flex: 1, minWidth: 240 }}>
          <Typography sx={{ fontSize: 18, fontWeight: 600, lineHeight: 1.3, overflowWrap: "anywhere" }}>
            {entry.title}
          </Typography>
          <Typography variant="monoSmall" sx={{ color: colors.text.secondary, display: "block", mt: "2px" }}>
            {setRef(entry.set_id, entry.version)}
            {entry.family ? ` · ${entry.family}` : ""}
          </Typography>
          <Origin entry={entry} />
        </Box>
        <Chip
          size="small"
          variant="outlined"
          color="warning"
          label={t("queue.received", { when: formatAgo(t, entry.created_at) })}
          title={formatStamp(entry.created_at)}
        />
      </Box>

      {lineage && (
        <Alert
          severity={lineage.kind === "replaces" ? "info" : "success"}
          variant="outlined"
          action={
            canCompare ? (
              <Link component="button" type="button" underline="hover" variant="body2" onClick={() => setCompare((v) => !v)}>
                {compare ? t("queue.hideCompare") : t("queue.compare", { version: lineage.current_version })}
              </Link>
            ) : undefined
          }
        >
          {t(`queue.lineage.${lineage.kind}`, { version: lineage.current_version })}
        </Alert>
      )}
      {canCompare && (
        <Collapse in={compare} unmountOnExit>
          <Comparison entry={entry} />
        </Collapse>
      )}
      <EditedNotice entry={entry} />

      <EntryFacts entry={entry} />

      <Divider sx={{ borderColor: colors.border.light }} />
      <Stack direction="row" spacing={1} useFlexGap flexWrap="wrap">
        <Button variant="contained" color="success" size="small" disabled={moderation.busy} onClick={() => moderation.approve(entry)}>
          {t("queue.approve")}
        </Button>
        <Button variant="outlined" color="primary" size="small" disabled={moderation.busy} onClick={() => moderation.edit(entry)}>
          {t("queue.edit")}
        </Button>
        <Button variant="outlined" color="error" size="small" disabled={moderation.busy} onClick={() => moderation.reject(entry)}>
          {t("queue.reject")}
        </Button>
        <Button variant="outlined" color="inherit" size="small" disabled={moderation.busy} onClick={() => moderation.hide(entry)}>
          {t("queue.hide")}
        </Button>
        <Box sx={{ flex: 1 }} />
        <Button variant="text" color="error" size="small" disabled={moderation.busy} onClick={() => moderation.ban(entry.uploader_hmac, entry.author)}>
          {t("queue.banUploader")}
        </Button>
      </Stack>
    </Paper>
  );
}
