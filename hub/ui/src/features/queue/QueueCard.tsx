import { Alert, Box, Button, Checkbox, Chip, Collapse, Divider, Link, Paper, Stack, Typography } from "@mui/material";
import { forwardRef, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { colors, radiusPx } from "@design";
import type { EntryView } from "@/models/api";
import { useSetDetail } from "@/features/sets/api";
import { formatAgo, formatStamp, setRef } from "@/shared/utils/format";
import { EditedNotice } from "@/features/sets/EditedNotice";
import { EntryFacts, Origin } from "@/features/sets/EntryFacts";
import { ProjectionDiff } from "@/features/sets/ProjectionDiff";
import { TechniqueChips } from "@/features/sets/components/TechniqueChips";
import type { Moderation } from "@/features/moderation/useModeration";

interface QueueCardProps {
  entry: EntryView;
  moderation: Moderation;
  focused: boolean;
  selected: boolean;
  compare: boolean;
  onToggleSelect: () => void;
  onToggleCompare: () => void;
  onOpen: () => void;
  onFocus: () => void;
  similar?: ReactNode;
}

function Comparison({ entry }: Readonly<{ entry: EntryView }>) {
  const detail = useSetDetail(entry.set_id);
  const current = entry.lineage?.current_version;
  const listed = detail.data?.versions.find((v) => v.version === current);
  if (!listed || current === undefined) return null;
  return <ProjectionDiff before={listed.projection} after={entry.projection} beforeVersion={current} />;
}

export const QueueCard = forwardRef<HTMLDivElement, QueueCardProps>(function QueueCard(
  { entry, moderation, focused, selected, compare, onToggleSelect, onToggleCompare, onOpen, onFocus, similar },
  ref,
) {
  const { t } = useTranslation();
  const lineage = entry.lineage;
  const canCompare = (lineage?.kind === "replaces" || lineage?.kind === "older") && lineage.current_version !== undefined;
  const severity = lineage?.kind === "older" ? "warning" : lineage?.kind === "replaces" ? "info" : "success";

  return (
    <Paper
      ref={ref}
      variant="outlined"
      onMouseDown={onFocus}
      sx={{
        p: "20px 24px",
        bgcolor: colors.background.paper,
        border: `1px solid ${focused ? colors.primary : colors.border.default}`,
        boxShadow: focused ? `0 0 0 1px ${colors.primary}` : "none",
        scrollMarginTop: 16,
        borderRadius: `${radiusPx.md}px`,
        display: "flex",
        flexDirection: "column",
        gap: 1.5,
        minWidth: 0,
      }}
    >
      <Box sx={{ display: "flex", alignItems: "flex-start", gap: 2, flexWrap: "wrap" }}>
        <Checkbox size="small" checked={selected} onChange={onToggleSelect} sx={{ p: 0.5, mt: -0.25 }} />
        <Box sx={{ flex: 1, minWidth: 240 }}>
          <Link component="button" type="button" underline="hover" onClick={onOpen} sx={{ fontSize: 18, fontWeight: 600, lineHeight: 1.3, overflowWrap: "anywhere", textAlign: "left", color: colors.text.primary }}>
            {entry.title}
          </Link>
          <Typography variant="monoSmall" sx={{ color: colors.text.secondary, display: "block", mt: "2px" }}>
            {setRef(entry.set_id, entry.version)}
          </Typography>
          <Box sx={{ mt: 0.75 }}>
            <TechniqueChips terms={entry.strategy} max={8} />
          </Box>
          <Origin entry={entry} />
        </Box>
        <Stack direction="row" spacing={1} useFlexGap flexWrap="wrap" justifyContent="flex-end">
          {entry.author_banned && <Chip size="small" color="error" label={t("queue.authorBanned")} />}
          {entry.withheld === "set_withdrawn" && <Chip size="small" color="warning" label={t("queue.setWithdrawn")} />}
          <Chip
            size="small"
            variant="outlined"
            color="warning"
            label={t("queue.received", { when: formatAgo(t, entry.created_at) })}
            title={formatStamp(entry.created_at)}
          />
        </Stack>
      </Box>

      {lineage && (
        <Alert
          severity={severity}
          variant="outlined"
          action={
            canCompare ? (
              <Link component="button" type="button" underline="hover" variant="body2" onClick={onToggleCompare}>
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
      {similar}

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
        {!entry.author_banned && (
          <Button variant="text" color="error" size="small" disabled={moderation.busy} onClick={() => moderation.ban(entry.uploader_hmac, entry.author)}>
            {t("queue.banUploader")}
          </Button>
        )}
      </Stack>
    </Paper>
  );
});
