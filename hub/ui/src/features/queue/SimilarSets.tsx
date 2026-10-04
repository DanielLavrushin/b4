import { Alert, Box, Button, Chip, CircularProgress, Stack, Typography } from "@mui/material";
import { useQuery, type UseQueryResult } from "@tanstack/react-query";
import type { TFunction } from "i18next";
import { useTranslation } from "react-i18next";
import { colors, spacing } from "@design";
import { get } from "@/api/client";
import type { EntryView, SimilarItemView, SimilarView } from "@/models/api";
import { tipLines } from "@/features/sets/card/badges";
import type { CardBadge } from "@/features/sets/card/types";
import { SetRef } from "@/shared/components/SetRef";
import { setRef } from "@/shared/utils/format";
import { errorText } from "@/shared/utils/notices";

export const SIMILAR_PANEL = "similar";

const duplicateCodes = ["same_targets", "same_title"];

export const useSimilar = (id: string, version: number) =>
  useQuery({
    queryKey: ["sets", "similar", id, version],
    queryFn: () => get<SimilarView>(`/sets/${encodeURIComponent(id)}/${String(version)}/similar`),
    staleTime: 60_000,
  });

const duplicateSignals = (item: SimilarItemView): string[] =>
  item.relations.some((r) => r.code === "same_set") ? [] : duplicateCodes.filter((code) => item.relations.some((r) => r.code === code));

const relationText = (t: TFunction, item: SimilarItemView) => {
  const strong = duplicateSignals(item);
  return {
    strong: strong.map((code) => t(`similar.relation.${code}`)),
    others: item.relations.filter((r) => !strong.includes(r.code)).map((r) => ({ code: r.code, label: t(`similar.relation.${r.code}`, { count: r.count ?? 0 }) })),
  };
};

const hasStrong = (items: SimilarItemView[]) => items.some((item) => duplicateSignals(item).length > 0);

export function similarBadge(t: TFunction, similar: UseQueryResult<SimilarView>): CardBadge | null {
  if (similar.isPending) return null;
  const items = similar.data?.items ?? [];
  if (items.length === 0) {
    if (!similar.isError) return null;
    return {
      key: SIMILAR_PANEL,
      label: t("similar.badgeFailed"),
      tone: "warning",
      panel: SIMILAR_PANEL,
      tooltip: t("similar.failed", { message: errorText(t, similar.error) }),
    };
  }
  return {
    key: SIMILAR_PANEL,
    label: t("similar.badge", { count: items.length }),
    tone: hasStrong(items) ? "warning" : "default",
    panel: SIMILAR_PANEL,
    tooltip: tipLines(
      items.map((item) => {
        const rel = relationText(t, item);
        return `${item.title} v${String(item.version)}: ${[...rel.strong, ...rel.others.map((o) => o.label)].join(", ")}`;
      }),
    ),
  };
}

function SimilarItem({ entry, item, onReject }: Readonly<{ entry: EntryView; item: SimilarItemView; onReject: (e: EntryView, reason?: string) => void }>) {
  const { t } = useTranslation();
  const rel = relationText(t, item);
  const caption = [
    `+${String(item.votes.works)} / -${String(item.votes.broken)}`,
    item.shared.map((s) => s.replace(/^[a-z]+:/, "")).join(", "),
    item.status_reason ? t("similar.reason", { reason: item.status_reason }) : "",
  ].filter(Boolean);
  return (
    <Box sx={{ py: spacing.sm, minWidth: 0, "& + &": { borderTop: `1px solid ${colors.border.light}` } }}>
      <Box sx={{ display: "flex", minWidth: 0 }}>
        <SetRef id={item.set_id} version={item.version} title={item.title} status={item.status} />
      </Box>
      <Box sx={{ display: "flex", gap: spacing.xs, flexWrap: "wrap", mt: spacing.xs }}>
        {item.listed && <Chip size="small" color="secondary" label={t("similar.listed")} />}
        {rel.strong.length > 0 && <Chip size="small" variant="outlined" color="warning" label={rel.strong.join(", ")} />}
        {rel.others.map((r) => (
          <Chip key={r.code} size="small" variant="outlined" label={r.label} />
        ))}
      </Box>
      <Box sx={{ display: "flex", alignItems: "center", gap: spacing.sm, mt: spacing.xs, minWidth: 0 }}>
        <Typography variant="caption" sx={{ flex: 1, minWidth: 0, color: colors.text.secondary, overflowWrap: "anywhere" }}>
          {caption.join(" · ")}
        </Typography>
        <Button
          size="small"
          onClick={() => onReject(entry, t("similar.duplicateReason", { ref: setRef(item.set_id, item.version), title: item.title }))}
          sx={{ flexShrink: 0, color: colors.text.secondary, px: 1, py: 0.25, mr: -1, whiteSpace: "nowrap" }}
        >
          {t("similar.rejectAsDuplicate")}
        </Button>
      </Box>
    </Box>
  );
}

export const similarLabel = (t: TFunction, similar: UseQueryResult<SimilarView>): string | undefined => {
  if (similar.isPending) return undefined;
  const count = similar.data?.items.length ?? 0;
  return similar.isError && count === 0 ? t("similar.badgeFailed") : t("similar.title", { count });
};

interface SimilarListProps {
  entry: EntryView;
  similar: UseQueryResult<SimilarView>;
  onReject: (e: EntryView, reason?: string) => void;
}

export function SimilarList({ entry, similar, onReject }: Readonly<SimilarListProps>) {
  const { t } = useTranslation();
  const items = similar.data?.items ?? [];
  if (similar.isPending) {
    return (
      <Stack direction="row" alignItems="center" spacing={1}>
        <CircularProgress size={12} sx={{ color: colors.secondary }} />
        <Typography variant="caption" sx={{ color: colors.text.secondary }}>
          {t("similar.checking")}
        </Typography>
      </Stack>
    );
  }
  if (similar.isError && items.length === 0) {
    return (
      <Alert severity="warning">
        {t("similar.failed", { message: errorText(t, similar.error) })}
        <Box>
          <Button color="inherit" size="small" onClick={() => void similar.refetch()} sx={{ mt: 0.5, ml: -1, px: 1, minWidth: 0 }}>
            {t("app.retry")}
          </Button>
        </Box>
      </Alert>
    );
  }
  return (
    <Box sx={{ minWidth: 0, mt: -spacing.sm }}>
      {items.map((item) => (
        <SimilarItem key={setRef(item.set_id, item.version)} entry={entry} item={item} onReject={onReject} />
      ))}
      {similar.data?.truncated && (
        <Typography variant="caption" sx={{ display: "block", pt: spacing.sm, borderTop: `1px solid ${colors.border.light}`, color: colors.text.secondary }}>
          {t("similar.truncated")}
        </Typography>
      )}
    </Box>
  );
}
