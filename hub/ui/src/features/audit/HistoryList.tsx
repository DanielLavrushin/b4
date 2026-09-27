import { Box, Stack, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";
import { colors } from "@design";
import type { AuditEntryView } from "@/models/api";
import { EmptyState } from "@/shared/components/States";
import { formatAgo, formatStamp } from "@/shared/utils/format";

export const changeSummary = (entry: AuditEntryView): string => {
  const before = entry.before ?? {};
  const after = entry.after ?? {};
  const keys = new Set([...Object.keys(before), ...Object.keys(after)]);
  const parts: string[] = [];
  keys.forEach((k) => {
    const a = before[k];
    const b = after[k];
    if (JSON.stringify(a) === JSON.stringify(b)) return;
    const show = (v: unknown) => (v === undefined ? "" : typeof v === "string" ? v : JSON.stringify(v));
    parts.push(a === undefined ? `${k}: ${show(b)}` : b === undefined ? `${k}: ${show(a)}` : `${k}: ${show(a)} -> ${show(b)}`);
  });
  return parts.join(", ");
};

export const actorText = (t: (k: string, o?: Record<string, unknown>) => string, entry: AuditEntryView): string =>
  `${t(`audit.actor.${entry.actor}`, { defaultValue: entry.actor })}${entry.actor_ref ? ` ${entry.actor_ref}` : ""}`;

export function HistoryList({ entries }: Readonly<{ entries: AuditEntryView[] }>) {
  const { t } = useTranslation();
  if (entries.length === 0) return <EmptyState text={t("audit.empty")} />;
  return (
    <Stack spacing={1}>
      {entries.map((e) => (
        <Box key={e.id} sx={{ borderLeft: `2px solid ${colors.border.default}`, pl: 1.5 }}>
          <Typography variant="body2">
            <Box component="span" sx={{ fontWeight: 600 }}>
              {t(`audit.action.${e.action}`, { defaultValue: e.action })}
            </Box>
            {e.version ? ` v${String(e.version)}` : ""}
            {e.reason ? ` · ${e.reason}` : ""}
          </Typography>
          <Typography variant="caption" sx={{ color: colors.text.secondary, display: "block" }}>
            <span title={formatStamp(e.at)}>{formatAgo(t, e.at)}</span> · {actorText(t, e)}
            {changeSummary(e) ? ` · ${changeSummary(e)}` : ""}
          </Typography>
        </Box>
      ))}
    </Stack>
  );
}
