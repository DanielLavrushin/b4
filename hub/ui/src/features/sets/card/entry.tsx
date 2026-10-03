import type { ReactNode } from "react";
import type { TFunction } from "i18next";
import { useTranslation } from "react-i18next";
import type { FacetSetConfig } from "@design";
import type { EntryView } from "@/models/api";
import { useKnownKey } from "@/features/keys/KnownKeys";
import { formatAgo, formatStamp } from "@/shared/utils/format";
import { techniqueText } from "@/shared/utils/terms";
import { EDITED_PANEL, bannedBadge, editedBadge, flagBadges, optionalTip, reportsBadge, tipBlocks, withdrawnBadge } from "./badges";
import { EditedDiff } from "./EditedDiff";
import { asSetConfig, targetPreviewLines, targetSummaryOf } from "./summary";
import type { CardBadge, CardPanel } from "./types";

export interface EntryCardParts {
  config?: FacetSetConfig;
  targetText?: string;
  targetTooltip?: ReactNode;
  meta: string[];
  metaTooltip: ReactNode;
  flags: CardBadge[];
  banned: CardBadge | null;
  withdrawn: CardBadge | null;
  edited: CardBadge | null;
  reports: CardBadge | null;
  editedPanel: CardPanel | null;
}

export const entryOrigin = (t: TFunction, entry: EntryView): string[] => {
  const lines = [t("queue.received", { when: formatStamp(entry.created_at) })];
  if (entry.asn_observed) lines.push(t("entry.seenFrom", { asn: entry.asn_observed, country: entry.country_observed ?? "" }).trim());
  if (entry.asn_hint) lines.push(t("entry.claims", { asn: entry.asn_hint, country: entry.country_hint ?? "" }).trim());
  if (entry.b4_version) {
    lines.push(
      entry.engine
        ? t("entry.sharedFrom", { version: entry.b4_version, engine: entry.engine })
        : t("entry.sharedFromVersion", { version: entry.b4_version }),
    );
  }
  return lines;
};

export function useEntryCard(entry: EntryView, onOpen?: () => void): EntryCardParts {
  const { t } = useTranslation();
  const known = useKnownKey(entry.uploader_hmac);
  const config = asSetConfig(entry.config);
  const comparable = entry.original_projection !== undefined;
  return {
    config,
    targetText: config ? undefined : targetSummaryOf(t, entry.targets),
    targetTooltip: optionalTip(targetPreviewLines(t, entry.targets)),
    meta: [
      t("card.by", { author: known?.name || entry.author }),
      entry.family ?? "",
      entry.b4_min ? t("card.needsB4", { version: entry.b4_min }) : "",
      t("queue.received", { when: formatAgo(t, entry.created_at) }),
    ],
    metaTooltip: tipBlocks([entryOrigin(t, entry), entry.strategy.map((term) => techniqueText(t, term))]),
    flags: flagBadges(t, entry.flags),
    banned: entry.author_banned ? bannedBadge(t) : null,
    withdrawn: entry.withheld === "set_withdrawn" ? withdrawnBadge(t) : null,
    edited: editedBadge(t, entry, comparable ? EDITED_PANEL : undefined),
    reports: reportsBadge(t, { open: entry.open_reports, total: entry.reports.length, independent: entry.independent_reports }, onOpen),
    editedPanel: comparable ? { key: EDITED_PANEL, label: t("edit.moderatorDiffTitle"), content: <EditedDiff entry={entry} compact /> } : null,
  };
}
