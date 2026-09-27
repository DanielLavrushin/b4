import { Box, Tooltip, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";
import { colors } from "@design";
import type { VoteRowView } from "@/models/api";
import { formatStamp } from "@/shared/utils/format";

const fmt = (n: number, digits = 2) => (Number.isInteger(n) ? String(n) : n.toFixed(digits));

export function AppliedWeight({ vote }: Readonly<{ vote: VoteRowView }>) {
  const { t } = useTranslation();
  const a = vote.applied;
  const counted = a.state === "counted";
  const breakdown = t("feedback.applied.breakdown", {
    base: fmt(a.base, 1),
    origin: fmt(a.origin),
    young: fmt(a.young_key),
    decay: fmt(a.decay),
    weight: fmt(a.weight, 3),
    at: a.at ? formatStamp(a.at) : t("feedback.applied.now"),
  });
  const state = t(`feedback.applied.state.${a.state}`, { reason: a.reason ? t(`feedback.applied.reason.${a.reason}`, { defaultValue: a.reason }) : "" });
  return (
    <Tooltip title={<Box sx={{ whiteSpace: "pre-line" }}>{`${breakdown}\n${state}${vote.author_vote ? `\n${t("feedback.authorVote")}` : ""}`}</Box>}>
      <Box sx={{ whiteSpace: "nowrap", textAlign: "right" }}>
        <Typography
          component="span"
          variant="body2"
          sx={{ color: !counted ? colors.text.disabled : a.weight >= 0 ? colors.state.success : colors.state.error, textDecoration: counted ? "none" : "line-through" }}
        >
          {a.weight > 0 ? "+" : ""}
          {fmt(a.weight, 3)}
        </Typography>
        {!counted && (
          <Typography variant="caption" sx={{ display: "block", color: colors.text.secondary }}>
            {t(`feedback.applied.short.${a.state}`)}
          </Typography>
        )}
        {vote.author_vote && (
          <Typography variant="caption" sx={{ display: "block", color: colors.text.disabled }}>
            {t("feedback.authorShort")}
          </Typography>
        )}
      </Box>
    </Tooltip>
  );
}
