import { Box, Button, CircularProgress, Stack, Typography } from "@mui/material";
import CompareArrowsIcon from "@mui/icons-material/CompareArrows";
import InfoIcon from "@mui/icons-material/Info";
import PersonOffIcon from "@mui/icons-material/PersonOff";
import { useTranslation } from "react-i18next";
import { colors } from "@design";
import type { EntryView } from "@/models/api";
import { useSetDetail } from "@/features/sets/api";
import { ProjectionDiff } from "@/features/sets/ProjectionDiff";
import { setPath } from "@/features/sets/SetDrawerHost";
import { isBadge } from "@/features/sets/card/badges";
import { useEntryCard } from "@/features/sets/card/entry";
import { SetCard } from "@/features/sets/card/SetCard";
import type { CardMenuItem, CardPanel } from "@/features/sets/card/types";
import type { Moderation } from "@/features/moderation/useModeration";
import { ErrorState } from "@/shared/components/States";
import { useOverlay } from "@/shared/hooks/useOverlay";
import { SIMILAR_PANEL, SimilarList, similarBadge, similarLabel, useSimilar } from "./SimilarSets";

export const COMPARE_PANEL = "compare";

const actionPair = { display: "flex", gap: 1 } as const;
const actionButton = { px: 1.5 } as const;

function ListedComparison({ entry, listed }: Readonly<{ entry: EntryView; listed: number }>) {
  const { t } = useTranslation();
  const detail = useSetDetail(entry.set_id);
  if (detail.isPending) {
    return (
      <Stack direction="row" alignItems="center" spacing={1}>
        <CircularProgress size={12} sx={{ color: colors.secondary }} />
        <Typography variant="caption" sx={{ color: colors.text.secondary }}>
          {t("app.loading")}
        </Typography>
      </Stack>
    );
  }
  if (detail.isError) return <ErrorState error={detail.error} onRetry={() => void detail.refetch()} />;
  const version = detail.data.versions.find((v) => v.version === listed);
  if (!version) return null;
  return <ProjectionDiff before={version.projection} after={entry.projection} beforeVersion={listed} />;
}

interface QueueCardProps {
  entry: EntryView;
  moderation: Moderation;
  selected: boolean;
  onToggleSelect: () => void;
  panel: string | null;
  onPanelChange: (panel: string | null) => void;
}

export function QueueCard({ entry, moderation, selected, onToggleSelect, panel, onPanelChange }: Readonly<QueueCardProps>) {
  const { t } = useTranslation();
  const overlay = useOverlay();
  const open = () => overlay.open(setPath(entry.set_id, entry.version));
  const parts = useEntryCard(entry, open);
  const similar = useSimilar(entry.set_id, entry.version);
  const lineage = entry.lineage;
  const listed = lineage?.current_version;
  const panels: CardPanel[] = [
    { key: SIMILAR_PANEL, label: similarLabel(t, similar), content: <SimilarList entry={entry} similar={similar} onReject={moderation.reject} /> },
  ];
  if (listed !== undefined) {
    panels.push({ key: COMPARE_PANEL, label: t("queue.compare", { version: listed }), content: <ListedComparison entry={entry} listed={listed} /> });
  }
  if (parts.editedPanel) panels.push(parts.editedPanel);

  const menu: CardMenuItem[] = [{ key: "open", label: t("queue.openDetails"), icon: <InfoIcon fontSize="small" />, onClick: open }];
  if (listed !== undefined) {
    menu.push({
      key: "compare",
      label: t("queue.compare", { version: listed }),
      activeLabel: t("queue.hideCompare"),
      icon: <CompareArrowsIcon fontSize="small" />,
      panel: COMPARE_PANEL,
    });
  }
  if (!entry.author_banned) {
    menu.push({
      key: "ban",
      label: t("queue.banUploader"),
      icon: <PersonOffIcon fontSize="small" />,
      onClick: () => moderation.ban(entry.uploader_hmac, entry.author),
      divider: true,
      accent: true,
      disabled: moderation.busy,
    });
  }

  return (
    <SetCard
      setId={entry.set_id}
      title={entry.title}
      config={parts.config}
      targetText={parts.targetText}
      version={{
        version: entry.version,
        tooltip: lineage ? t(`queue.lineage.${lineage.kind}`, { version: listed }) : undefined,
        warning: lineage?.kind === "older",
      }}
      meta={parts.meta}
      metaTooltip={parts.metaTooltip}
      description={entry.description}
      badges={[...parts.flags, parts.banned, parts.withdrawn, parts.edited, similarBadge(t, similar), parts.reports].filter(isBadge)}
      panels={panels}
      panel={panel}
      onPanelChange={onPanelChange}
      menu={menu}
      selection={{ selected, onToggle: onToggleSelect, label: t("card.select", { title: entry.title }) }}
      onOpen={open}
      actions={
        <>
          <Box sx={actionPair}>
            <Button variant="contained" size="small" disabled={moderation.busy} onClick={() => moderation.approve(entry)} sx={actionButton}>
              {t("queue.approve")}
            </Button>
            <Button variant="outlined" size="small" disabled={moderation.busy} onClick={() => moderation.edit(entry)} sx={actionButton}>
              {t("queue.edit")}
            </Button>
          </Box>
          <Box sx={actionPair}>
            <Button variant="outlined" size="small" disabled={moderation.busy} onClick={() => moderation.reject(entry)} sx={actionButton}>
              {t("queue.reject")}
            </Button>
            <Button variant="outlined" size="small" disabled={moderation.busy} onClick={() => moderation.hide(entry)} sx={actionButton}>
              {t("queue.hide")}
            </Button>
          </Box>
        </>
      }
    />
  );
}
