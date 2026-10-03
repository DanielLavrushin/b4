import { Box } from "@mui/material";
import EditIcon from "@mui/icons-material/Edit";
import type { TFunction } from "i18next";
import { formatAgo, formatStamp } from "@/shared/utils/format";
import { flagHint, flagText } from "@/shared/utils/terms";
import type { CardBadge, CardTone } from "./types";

export const EDITED_PANEL = "edited";

export const isBadge = (badge: CardBadge | null | undefined | false): badge is CardBadge => Boolean(badge);

export const tipLines = (lines: readonly string[]) => <Box sx={{ whiteSpace: "pre-line" }}>{lines.filter(Boolean).join("\n")}</Box>;

export const optionalTip = (lines: readonly string[]) => (lines.some(Boolean) ? tipLines(lines) : undefined);

export const tipBlocks = (blocks: readonly (readonly string[])[]) => (
  <Box sx={{ whiteSpace: "pre-line" }}>
    {blocks
      .map((lines) => lines.filter(Boolean))
      .filter((lines) => lines.length > 0)
      .map((lines, i) => (
        <Box key={`${String(i)}-${lines[0] ?? ""}`} sx={i > 0 ? { mt: 0.75 } : undefined}>
          {lines.join("\n")}
        </Box>
      ))}
  </Box>
);

export const flagTone = (flag: string): CardTone => {
  if (flag === "block") return "error";
  if (flag === "catch_all" || flag === "blanket") return "warning";
  return "default";
};

export const flagBadges = (t: TFunction, flags: readonly string[]): CardBadge[] =>
  flags.map((flag) => ({ key: `flag:${flag}`, label: flagText(t, flag), tone: flagTone(flag), tooltip: flagHint(t, flag) || undefined }));

export const bannedBadge = (t: TFunction): CardBadge => ({
  key: "author-banned",
  label: t("queue.authorBanned"),
  tone: "error",
  tooltip: t("card.bannedTip"),
});

export const withdrawnBadge = (t: TFunction): CardBadge => ({
  key: "set-withdrawn",
  label: t("queue.setWithdrawn"),
  tone: "warning",
  tooltip: t("card.withdrawnTip"),
});

interface EditMarks {
  edited_at?: string;
  edit_note?: string;
}

export const editedBadge = (t: TFunction, edit: EditMarks, panel?: string): CardBadge | null => {
  if (!edit.edited_at) return null;
  return {
    key: "edited",
    label: t("card.edited"),
    icon: <EditIcon />,
    panel,
    tooltip: tipLines([
      `${t("queue.edited", { when: formatAgo(t, edit.edited_at) })}, ${formatStamp(edit.edited_at)}`,
      edit.edit_note ? t("queue.editedNote", { note: edit.edit_note }) : "",
    ]),
  };
};

interface ReportCounts {
  open: number;
  total: number;
  independent: number;
}

export const reportsBadge = (t: TFunction, counts: ReportCounts, onClick?: () => void): CardBadge | null => {
  if (counts.open <= 0) return null;
  return {
    key: "reports",
    label: t("card.reports", { count: counts.open }),
    tone: "warning",
    tooltip: t("card.reportsTip", { open: counts.open, total: counts.total, independent: counts.independent }),
    onClick,
  };
};
