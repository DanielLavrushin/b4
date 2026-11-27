import { Box, Button, Chip, Grid, Link, Tooltip, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";
import { colors, radius } from "@design";
import type { GroupVersionView } from "@/models/api";
import { useKnownKey } from "@/features/keys/KnownKeys";
import { GroupActions, MemberCaption, MemberRow, groupOrigin } from "@/features/groups/GroupParts";
import { groupPath } from "@/features/groups/GroupDrawerHost";
import { tipLines } from "@/features/sets/card/badges";
import type { Moderation } from "@/features/moderation/useModeration";
import { useOverlay } from "@/shared/hooks/useOverlay";
import { formatAgo, setRef } from "@/shared/utils/format";
import { QueueCard } from "./QueueCard";

interface QueueGroupProps {
  group: GroupVersionView;
  moderation: Moderation;
  panelOf: (key: string) => string | null;
  setPanel: (key: string, panel: string | null) => void;
}

export function QueueGroup({ group, moderation, panelOf, setPanel }: Readonly<QueueGroupProps>) {
  const { t } = useTranslation();
  const overlay = useOverlay();
  const known = useKnownKey(group.author_hmac);
  const open = () => overlay.open(groupPath(group.group_id, group.version));
  const meta = [t("card.by", { author: known?.name || group.author }), t("queue.received", { when: formatAgo(t, group.created_at) })].join(" · ");

  return (
    <Box sx={{ border: `1px solid ${colors.border.default}`, borderRadius: radius.md, p: 2, display: "flex", flexDirection: "column", gap: 2 }}>
      <Box sx={{ display: "flex", flexDirection: "column", gap: 0.5, minWidth: 0 }}>
        <Typography variant="metricLabel">{t("groups.label")}</Typography>
        <Link component="button" type="button" underline="hover" onClick={open} sx={{ textAlign: "left", color: colors.text.primary, alignSelf: "flex-start", maxWidth: "100%" }}>
          <Typography component="span" sx={{ fontSize: 18, fontWeight: 600, overflowWrap: "anywhere" }}>
            {t("groups.heading", { title: group.title, count: group.members.length })}
          </Typography>
        </Link>
        <Tooltip title={tipLines(groupOrigin(t, group))}>
          <Typography variant="caption" sx={{ color: colors.text.secondary, alignSelf: "flex-start" }}>
            {meta}
          </Typography>
        </Tooltip>
        {group.author_banned && <Chip size="small" variant="outlined" color="error" label={t("queue.authorBanned")} sx={{ alignSelf: "flex-start" }} />}
        {group.description && (
          <Typography variant="body2" sx={{ color: colors.text.secondary, overflowWrap: "anywhere" }}>
            {group.description}
          </Typography>
        )}
      </Box>
      <Box sx={{ display: "flex", alignItems: "center", gap: 1, flexWrap: "wrap" }}>
        <GroupActions group={group} moderation={moderation} />
        <Box sx={{ flex: 1 }} />
        <Button size="small" onClick={open} sx={{ color: colors.text.secondary }}>
          {t("queue.openDetails")}
        </Button>
      </Box>
      <Grid container spacing={2}>
        {group.members.map((m) => {
          const entry = m.entry;
          if (!entry) {
            return (
              <Grid key={m.position} size={{ xs: 12, md: 6, xl: 4 }}>
                <MemberRow group={group} member={m} />
              </Grid>
            );
          }
          const key = setRef(entry.set_id, entry.version);
          return (
            <Grid key={m.position} size={{ xs: 12, md: 6, xl: 4 }} sx={{ display: "flex", flexDirection: "column" }}>
              <MemberCaption group={group} member={m} />
              <Box sx={{ flex: 1, minWidth: 0 }}>
                <QueueCard entry={entry} moderation={moderation} panel={panelOf(key)} onPanelChange={(next) => setPanel(key, next)} />
              </Box>
            </Grid>
          );
        })}
      </Grid>
    </Box>
  );
}
