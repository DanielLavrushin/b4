import { Box, Button, LinearProgress, Stack, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";
import { StopIcon } from "@b4.icons";
import { colors } from "@design";
import type { DetectorSuite } from "@models/detector";

interface RunPanelProps {
  suite: DetectorSuite;
  onStop: () => void;
}

export const RunPanel = ({ suite, onStop }: RunPanelProps) => {
  const { t } = useTranslation();
  const p = suite.progress;
  const pct = p.total > 0 ? Math.min(100, (p.done / p.total) * 100) : 0;
  const stopping = suite.status === "canceled";
  const phase = p.phase ? t(`detector.scopes.${p.phase}.name`) : t("detector.run.preparing");
  const queued = (suite.options.scopes ?? [])
    .filter((s) => s !== p.phase)
    .filter((s) => {
      const key = s as keyof DetectorSuite;
      return !suite[key];
    })
    .map((s) => t(`detector.scopes.${s}.name`));

  return (
    <Stack spacing={1.5}>
      <Stack direction="row" spacing={2} alignItems="baseline" flexWrap="wrap" useFlexGap>
        <Typography sx={{ fontWeight: 600 }}>{phase}</Typography>
        <Typography variant="body2" sx={{ color: colors.text.secondary, fontVariantNumeric: "tabular-nums" }}>
          {t("detector.run.progress", { done: p.done, total: p.total })}
        </Typography>
        {p.current && (
          <Typography variant="body2" sx={{ fontFamily: "monospace", color: colors.text.primary }}>
            {p.current}
          </Typography>
        )}
        {queued.length > 0 && (
          <Typography variant="body2" sx={{ color: colors.text.disabled }}>
            {t("detector.run.queued", { scopes: queued.join(", ") })}
          </Typography>
        )}
      </Stack>
      <LinearProgress variant="determinate" value={pct} sx={{ height: 6, borderRadius: 3 }} />
      <Box>
        <Button variant="outlined" color="secondary" startIcon={<StopIcon />} onClick={onStop} disabled={stopping}>
          {stopping ? t("detector.run.stopping") : t("detector.run.stop")}
        </Button>
      </Box>
    </Stack>
  );
};
