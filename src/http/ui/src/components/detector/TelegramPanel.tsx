import { Box, Stack, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";
import { colors } from "@design";
import type { TelegramResult, TelegramThroughput } from "@models/detector";
import { StatusChip, telegramColor } from "./statuses";

const mb = (b: number) => (b / 1024 / 1024).toFixed(1);

const Throughput = ({ tp }: { tp: TelegramThroughput }) => {
  const { t } = useTranslation();
  if (!tp.verdict) return <StatusChip label={t("detector.status.PENDING")} color="default" />;
  const detail = [
    tp.mbps_avg > 0 && t("detector.telegram.speed", { mbps: tp.mbps_avg }),
    tp.bytes > 0 && t("detector.telegram.transferred", { mb: mb(tp.bytes), total: mb(tp.expected) }),
    tp.drop_at_sec ? t("detector.telegram.stalledAt", { sec: tp.drop_at_sec }) : "",
    tp.verdict === "blocked" && tp.detail,
  ]
    .filter(Boolean)
    .join(", ");
  return (
    <Stack direction="row" spacing={1} alignItems="center" flexWrap="wrap" useFlexGap>
      <StatusChip label={t(`detector.telegram.${tp.verdict}`)} color={telegramColor(tp.verdict)} />
      <Typography variant="body2" sx={{ color: colors.text.secondary }}>{detail}</Typography>
    </Stack>
  );
};

export const TelegramPanel = ({ result }: { result: TelegramResult }) => {
  const { t } = useTranslation();
  const dcColor = result.dc_total === 0 ? "default" : result.dc_reachable === result.dc_total ? "success" : result.dc_reachable === 0 ? "error" : "warning";
  return (
    <Box sx={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(240px, 1fr))", gap: 2 }}>
      <Stack spacing={0.5}>
        <Typography variant="caption" sx={{ color: colors.text.secondary, textTransform: "uppercase", letterSpacing: "0.05em" }}>
          {t("detector.telegram.datacenters")}
        </Typography>
        <Stack direction="row" spacing={1} alignItems="center" flexWrap="wrap" useFlexGap>
          <StatusChip label={t("detector.telegram.dcReachable", { ok: result.dc_reachable, total: result.dc_total })} color={dcColor} />
          <Typography variant="body2" sx={{ color: colors.text.secondary }}>
            {(result.dc_pings ?? []).map((p) => `DC${p.dc} ${p.ok ? `${p.rtt_ms} ms` : t("detector.telegram.dcDown")}`).join(" · ")}
          </Typography>
        </Stack>
      </Stack>
      <Stack spacing={0.5}>
        <Typography variant="caption" sx={{ color: colors.text.secondary, textTransform: "uppercase", letterSpacing: "0.05em" }}>
          {t("detector.telegram.download")}
        </Typography>
        <Throughput tp={result.download} />
      </Stack>
      <Stack spacing={0.5}>
        <Typography variant="caption" sx={{ color: colors.text.secondary, textTransform: "uppercase", letterSpacing: "0.05em" }}>
          {t("detector.telegram.upload")}
        </Typography>
        <Throughput tp={result.upload} />
      </Stack>
    </Box>
  );
};
