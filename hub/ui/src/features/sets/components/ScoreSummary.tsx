import { Box, Tooltip, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";
import { colors } from "@design";
import type { EvidenceView, ScoreView } from "@/models/api";
import { tipLines } from "@/features/sets/card/badges";
import { formatN } from "@/shared/utils/format";

const pct = (score: number) => `${String(Math.round(score * 100))}%`;

const scoreColor = (s: ScoreView) => {
  if (s.bucket === "none") return colors.text.disabled;
  if (s.score >= 0.7) return colors.state.success;
  if (s.score >= 0.4) return colors.state.warning;
  return colors.state.error;
};

const focusable = {
  borderRadius: 0.5,
  cursor: "default",
  "&:focus-visible": { outline: `1px solid ${colors.secondary}`, outlineOffset: 2 },
} as const;

interface ScoreSummaryProps {
  published?: ScoreView;
  live: ScoreView;
  evidence: EvidenceView;
}

export function ScoreSummary({ published, live, evidence }: Readonly<ScoreSummaryProps>) {
  const { t } = useTranslation();
  const shown = published ?? live;
  const drift = published !== undefined && Math.abs(published.score - live.score) > 0.01;
  const ind = evidence.independent;
  const own = evidence.author;
  const scoreTip = tipLines([
    published ? t("score.publishedTip", { score: pct(published.score), n: formatN(published.n), devices: published.devices }) : t("score.notPublished"),
    t("score.liveTip", { score: pct(live.score), n: formatN(live.n), devices: live.devices }),
    shown.bucket === "none" ? t("score.unratedTip") : "",
  ]);
  const evidenceTip = tipLines([
    t("evidence.independentTip", { works: ind.works, broken: ind.broken, devices: ind.devices, positive: formatN(ind.positive), negative: formatN(ind.negative) }),
    t("evidence.authorTip", { works: own.works, broken: own.broken }),
    evidence.pooled > 0 ? t("evidence.pooledTip", { count: evidence.pooled }) : "",
  ]);
  return (
    <Box sx={{ display: "flex", alignItems: "baseline", gap: 1, whiteSpace: "nowrap" }}>
      <Tooltip title={scoreTip} describeChild>
        <Typography component="span" variant="body2" tabIndex={0} sx={{ ...focusable, fontWeight: 600, color: scoreColor(shown) }}>
          {shown.bucket === "none" ? t("score.unrated") : pct(shown.score)}
          {drift && (
            <Typography component="span" variant="caption" sx={{ ml: 0.5, fontWeight: 400, color: colors.text.secondary }}>
              {t("score.next", { score: pct(live.score) })}
            </Typography>
          )}
        </Typography>
      </Tooltip>
      <Tooltip title={evidenceTip} describeChild>
        <Typography component="span" variant="caption" tabIndex={0} sx={{ ...focusable, color: colors.text.secondary }}>
          +{ind.works} / -{ind.broken}
        </Typography>
      </Tooltip>
    </Box>
  );
}
