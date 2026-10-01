import { Box, Tooltip, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";
import { colors } from "@design";
import type { EvidenceView, ScoreView } from "@/models/api";
import { formatN } from "@/shared/utils/format";

const pct = (score: number) => `${String(Math.round(score * 100))}%`;

const tone = (s: ScoreView) => {
  if (s.bucket === "none") return colors.text.disabled;
  if (s.score >= 0.7) return colors.state.success;
  if (s.score >= 0.4) return colors.state.warning;
  return colors.state.error;
};

export function ScoreCell({ published, live }: Readonly<{ published?: ScoreView; live: ScoreView }>) {
  const { t } = useTranslation();
  const shown = published ?? live;
  const drift = published !== undefined && Math.abs(published.score - live.score) > 0.01;
  const tip = [
    published ? t("score.publishedTip", { score: pct(published.score), n: formatN(published.n), devices: published.devices }) : t("score.notPublished"),
    t("score.liveTip", { score: pct(live.score), n: formatN(live.n), devices: live.devices }),
    shown.bucket === "none" ? t("score.unratedTip") : "",
  ]
    .filter(Boolean)
    .join("\n");
  return (
    <Tooltip title={<Box sx={{ whiteSpace: "pre-line" }}>{tip}</Box>}>
      <Box sx={{ whiteSpace: "nowrap" }}>
        <Typography component="span" variant="body2" sx={{ fontWeight: 600, color: tone(shown) }}>
          {shown.bucket === "none" ? t("score.unrated") : pct(shown.score)}
        </Typography>
        {drift && (
          <Typography component="span" variant="caption" sx={{ color: colors.text.secondary, ml: 0.5 }}>
            {t("score.next", { score: pct(live.score) })}
          </Typography>
        )}
        <Typography variant="caption" sx={{ display: "block", color: colors.text.secondary }}>
          {t("score.nDevices", { n: formatN(shown.n), devices: shown.devices })}
        </Typography>
      </Box>
    </Tooltip>
  );
}

export function EvidenceCell({ evidence }: Readonly<{ evidence: EvidenceView }>) {
  const { t } = useTranslation();
  const ind = evidence.independent;
  const own = evidence.author;
  return (
    <Tooltip
      title={
        <Box sx={{ whiteSpace: "pre-line" }}>
          {[
            t("evidence.independentTip", { works: ind.works, broken: ind.broken, devices: ind.devices, positive: formatN(ind.positive), negative: formatN(ind.negative) }),
            t("evidence.authorTip", { works: own.works, broken: own.broken }),
            evidence.pooled > 0 ? t("evidence.pooledTip", { count: evidence.pooled }) : "",
          ]
            .filter(Boolean)
            .join("\n")}
        </Box>
      }
    >
      <Box sx={{ whiteSpace: "nowrap" }}>
        <Typography component="span" variant="body2" sx={{ color: colors.state.success }}>
          +{ind.works}
        </Typography>{" "}
        <Typography component="span" variant="body2" sx={{ color: colors.state.error }}>
          -{ind.broken}
        </Typography>
        <Typography component="span" variant="caption" sx={{ color: colors.text.secondary, ml: 0.5 }}>
          {t("evidence.devices", { count: ind.devices })}
        </Typography>
        <Typography variant="caption" sx={{ display: "block", color: colors.text.disabled }}>
          {t("evidence.author", { works: own.works, broken: own.broken })}
          {evidence.pooled > 0 ? ` · ${t("evidence.pooled", { count: evidence.pooled })}` : ""}
        </Typography>
      </Box>
    </Tooltip>
  );
}
