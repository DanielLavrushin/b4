import { Tooltip, Typography } from "@mui/material";
import InfoIcon from "@mui/icons-material/Info";
import KeyIcon from "@mui/icons-material/Key";
import WarningIcon from "@mui/icons-material/Warning";
import type { TFunction } from "i18next";
import { useTranslation } from "react-i18next";
import { colors, spacing } from "@design";
import type { AttentionCode, SetGroupName, SetRowView } from "@/models/api";
import { useKnownKey } from "@/features/keys/KnownKeys";
import { ScoreSummary } from "@/features/sets/components/ScoreSummary";
import { setPath } from "@/features/sets/SetDrawerHost";
import { keyHref } from "@/shared/components/KeyRef";
import { useOverlay } from "@/shared/hooks/useOverlay";
import { formatAgo, formatStamp, setRef } from "@/shared/utils/format";
import { reasonText } from "@/shared/utils/reason";
import { bannedBadge, editedBadge, flagBadges, isBadge, reportsBadge, tipLines, withdrawnBadge } from "./badges";
import { SetCard } from "./SetCard";
import { asSetConfig, targetSummaryOf } from "./summary";
import type { CardBadge, CardMenuItem, CardSelection } from "./types";

const attentionRank: Record<string, number> = { low_score: 0, reports: 1, weak: 2, stale: 3 };

const rankOf = (code: string) => attentionRank[code] ?? Object.keys(attentionRank).length;

const unscored = new Set<SetGroupName>(["hidden", "rejected"]);

const attentionBadge = (t: TFunction, codes: readonly AttentionCode[]): CardBadge | null => {
  const sorted = [...codes].sort((a, b) => rankOf(a) - rankOf(b));
  const first = sorted[0];
  if (first === undefined) return null;
  const label = (code: string) => t(`attention.${code}.label`, { defaultValue: code });
  return {
    key: "attention",
    label: sorted.length > 1 ? `${label(first)} +${String(sorted.length - 1)}` : label(first),
    tone: "warning",
    icon: <WarningIcon />,
    tooltip: tipLines(sorted.map((code) => `${label(code)}: ${t(`attention.${code}.hint`, { defaultValue: "" })}`)),
  };
};

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

const rowNote = (t: TFunction, row: SetRowView, group: SetGroupName): Note | null => {
  if (group === "superseded" && row.superseded_by !== undefined) {
    const at = (when: string) => t("sets.supersededBy", { version: row.superseded_by, when });
    return { text: at(formatAgo(t, row.superseded_at)), tip: at(formatStamp(row.superseded_at)) };
  }
  let text = "";
  if (group === "hidden" || group === "rejected") text = t("sets.reasonLine", { reason: reasonText(t, row.status_reason, row.independent_reports) });
  if (group === "withheld" && row.withheld) text = t("sets.reasonLine", { reason: t(`sets.withheldReason.${row.withheld}`) });
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
  group: SetGroupName;
  selection?: CardSelection;
  panel: string | null;
  onPanelChange: (panel: string | null) => void;
}

export function RowCard({ row, group, selection, panel, onPanelChange }: Readonly<RowCardProps>) {
  const { t } = useTranslation();
  const overlay = useOverlay();
  const known = useKnownKey(row.author_hmac);
  const open = () => overlay.open(setPath(row.set_id));
  const config = asSetConfig(row.config);
  const awaiting = group === "hidden" && row.status_reason === "reports" && row.open_reports > 0;
  const attention = row.open_reports > 0 ? row.attention.filter((code) => code !== "reports") : row.attention;
  const note = rowNote(t, row, group);
  const hiddenFrom: CardBadge | null = row.hidden_from === "pending" ? { key: "hidden-from", label: t("detail.hiddenFromPending") } : null;
  const withheldTab = group === "withheld";
  const badges = [
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
      version={{
        version: row.version,
        tooltip: tipLines([setRef(row.set_id, row.version), row.versions && row.versions.length > 1 ? t("sets.versions", { list: row.versions.join(", ") }) : ""]),
      }}
      meta={[t("card.by", { author: known?.name || row.author }), row.family ?? "", t("sets.updatedAgo", { when: formatAgo(t, row.updated_at) })]}
      metaTooltip={tipLines([
        author.filter(Boolean).join(" · "),
        row.asn_observed ? t("entry.seenFrom", { asn: row.asn_observed, country: row.country_observed ?? "" }).trim() : "",
        t("queue.received", { when: formatStamp(row.created_at) }),
        t("sets.updatedAgo", { when: formatStamp(row.updated_at) }),
      ])}
      extra={note ? <RowNote note={note} /> : undefined}
      badges={badges}
      badgesEnd={unscored.has(group) ? undefined : <ScoreSummary published={row.published} live={row.live} evidence={row.evidence} />}
      panel={panel}
      onPanelChange={onPanelChange}
      menu={menu}
      selection={selection}
      onOpen={open}
    />
  );
}
