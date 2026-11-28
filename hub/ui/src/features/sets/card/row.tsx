import { Tooltip, Typography } from "@mui/material";
import GroupIcon from "@mui/icons-material/Category";
import InfoIcon from "@mui/icons-material/Info";
import KeyIcon from "@mui/icons-material/Key";
import WarningIcon from "@mui/icons-material/Warning";
import type { TFunction } from "i18next";
import { useTranslation } from "react-i18next";
import { colors, spacing } from "@design";
import type { AttentionCode, SetRowView, SetTabName } from "@/models/api";
import { groupPath } from "@/features/groups/GroupDrawerHost";
import { useKnownKey } from "@/features/keys/KnownKeys";
import { ScoreSummary } from "@/features/sets/components/ScoreSummary";
import { setPath } from "@/features/sets/SetDrawerHost";
import { keyHref } from "@/shared/components/KeyRef";
import { useOverlay } from "@/shared/hooks/useOverlay";
import { formatAgo, formatStamp, setRef } from "@/shared/utils/format";
import { reasonText } from "@/shared/utils/reason";
import { techniqueText } from "@/shared/utils/terms";
import { bannedBadge, editedBadge, flagBadges, isBadge, optionalTip, reportsBadge, tipBlocks, tipLines, withdrawnBadge } from "./badges";
import { SetCard } from "./SetCard";
import { asSetConfig, targetPreviewLines, targetSummaryOf } from "./summary";
import type { CardBadge, CardMenuItem, CardSelection } from "./types";

const attentionRank: Record<string, number> = { low_score: 0, reports: 1, weak: 2, stale: 3 };

const rankOf = (code: string) => attentionRank[code] ?? Object.keys(attentionRank).length;

const unscored = new Set<SetTabName>(["hidden", "rejected"]);

const attentionBadge = (t: TFunction, codes: readonly AttentionCode[]): CardBadge | null => {
  const sorted = [...codes].sort((a, b) => rankOf(a) - rankOf(b));
  const first = sorted[0];
  if (first === undefined) return null;
  const label = (code: string) => t(`attention.${code}.label`, { defaultValue: code });
  return {
    key: "attention",
    label: sorted.length > 1 ? `${label(first)} +${String(sorted.length - 1)}` : label(first),
    ariaLabel: sorted.map(label).join(", "),
    tone: "warning",
    icon: <WarningIcon />,
    tooltip: tipLines(sorted.map((code) => `${label(code)}: ${t(`attention.${code}.hint`, { defaultValue: "" })}`)),
  };
};

const groupBadges = (t: TFunction, row: SetRowView, openGroup: (path: string) => void): CardBadge[] =>
  (row.groups ?? []).slice(1).map((g) => ({
    key: `group:${g.group_id}`,
    label: t("card.group", { title: g.title }),
    icon: <GroupIcon />,
    tooltip: tipLines([
      t("card.groupTip", { title: g.title, version: g.version, status: t(`status.set.${g.status}`, { defaultValue: g.status }) }),
      t(g.brought ? "detail.groupBrought" : "detail.groupLinked", { version: g.set_version }),
      t("card.groupOpen"),
    ]),
    onClick: () => openGroup(groupPath(g.group_id, g.version)),
  }));

const reportsChip = (t: TFunction, row: SetRowView, awaiting: boolean, onOpen: () => void): CardBadge | null => {
  const counts = { open: row.open_reports, total: row.reports, independent: row.independent_reports };
  const open = reportsBadge(t, counts, onOpen);
  if (open) return awaiting ? { ...open, label: t("sets.awaitingReview") } : open;
  if (row.reports === 0) return null;
  return { key: "reports", label: t("sets.reportsTotal", { count: row.reports }), tooltip: t("card.reportsTip", counts), onClick: onOpen };
};

interface Note {
  text: string;
  tip: string;
}

const rowNote = (t: TFunction, row: SetRowView, tab: SetTabName): Note | null => {
  if (tab === "superseded" && row.superseded_by !== undefined) {
    const at = (when: string) => t("sets.supersededBy", { version: row.superseded_by, when });
    return { text: at(formatAgo(t, row.superseded_at)), tip: at(formatStamp(row.superseded_at)) };
  }
  let text = "";
  if (tab === "hidden" || tab === "rejected") text = t("sets.reasonLine", { reason: reasonText(t, row.status_reason, row.independent_reports) });
  if (tab === "withheld" && row.withheld) text = t("sets.reasonLine", { reason: t(`sets.withheldReason.${row.withheld}`) });
  return text ? { text, tip: text } : null;
};

function RowNote({ note }: Readonly<{ note: Note }>) {
  return (
    <Tooltip title={note.tip}>
      <Typography
        variant="body2"
        sx={{
          mt: spacing.sm,
          color: colors.text.secondary,
          display: "-webkit-box",
          WebkitBoxOrient: "vertical",
          WebkitLineClamp: 2,
          overflow: "hidden",
          overflowWrap: "anywhere",
        }}
      >
        {note.text}
      </Typography>
    </Tooltip>
  );
}

interface RowCardProps {
  row: SetRowView;
  tab: SetTabName;
  selection?: CardSelection;
  panel: string | null;
  onPanelChange: (panel: string | null) => void;
}

export function RowCard({ row, tab, selection, panel, onPanelChange }: Readonly<RowCardProps>) {
  const { t } = useTranslation();
  const overlay = useOverlay();
  const known = useKnownKey(row.author_hmac);
  const open = () => overlay.open(setPath(row.set_id, row.version));
  const config = asSetConfig(row.config);
  const awaiting = tab === "hidden" && row.status_reason === "reports" && row.open_reports > 0;
  const attention = row.open_reports > 0 ? row.attention.filter((code) => code !== "reports") : row.attention;
  const note = rowNote(t, row, tab);
  const hiddenFrom: CardBadge | null = row.hidden_from === "pending" ? { key: "hidden-from", label: t("detail.hiddenFromPending") } : null;
  const withheldTab = tab === "withheld";
  const badges = [
    ...groupBadges(t, row, overlay.open),
    ...flagBadges(t, row.flags),
    row.author_banned && !(withheldTab && row.withheld === "author_banned") ? bannedBadge(t) : null,
    row.withheld === "set_withdrawn" && !withheldTab ? withdrawnBadge(t) : null,
    hiddenFrom,
    editedBadge(t, row),
    attentionBadge(t, attention),
    reportsChip(t, row, awaiting, open),
  ].filter(isBadge);
  const menu: CardMenuItem[] = [
    { key: "open", label: t("queue.openDetails"), icon: <InfoIcon fontSize="small" />, onClick: open },
    { key: "author", label: t("sets.openAuthor"), icon: <KeyIcon fontSize="small" />, onClick: () => overlay.open(keyHref(row.author_hmac)) },
  ];
  const author = [
    known?.name,
    known?.tag ? t(`keys.tags.${known.tag}`, { defaultValue: known.tag }) : "",
    t("ref.authorLabel", { label: row.author }),
    known?.trusted ? t("ref.trusted") : "",
    row.author_banned ? t("ref.banned") : "",
  ];

  return (
    <SetCard
      setId={row.set_id}
      title={row.title}
      config={config}
      targetText={config ? undefined : targetSummaryOf(t, row.targets)}
      targetTooltip={optionalTip(targetPreviewLines(t, row.targets, config))}
      version={{
        version: row.version,
        tooltip: tipLines([setRef(row.set_id, row.version), row.versions && row.versions.length > 1 ? t("sets.versions", { list: row.versions.join(", ") }) : ""]),
      }}
      meta={[t("card.by", { author: known?.name || row.author }), row.family ?? "", t("sets.updatedAgo", { when: formatAgo(t, row.updated_at) })]}
      metaTooltip={tipBlocks([
        [
          author.filter(Boolean).join(" · "),
          row.asn_observed ? t("entry.seenFrom", { asn: row.asn_observed, country: row.country_observed ?? "" }).trim() : "",
          t("queue.received", { when: formatStamp(row.created_at) }),
          t("sets.updatedAgo", { when: formatStamp(row.updated_at) }),
        ],
        row.techniques.map((term) => techniqueText(t, term)),
      ])}
      extra={note ? <RowNote note={note} /> : undefined}
      badges={badges}
      badgesEnd={unscored.has(tab) ? undefined : <ScoreSummary published={row.published} live={row.live} evidence={row.evidence} />}
      panel={panel}
      onPanelChange={onPanelChange}
      menu={menu}
      selection={selection}
      onOpen={open}
    />
  );
}
