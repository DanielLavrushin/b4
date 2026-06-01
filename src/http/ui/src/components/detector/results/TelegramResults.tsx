import { Box, Stack, Typography } from "@mui/material";
import { colors } from "@design";
import { B4Badge } from "@b4.elements";
import { DownloadIcon } from "@b4.icons";
import type {
  TelegramResult,
  TelegramThroughput,
  TelegramVerdict,
} from "@models/detector";
import { ResultCard } from "../ResultCard";
import { StatusChip } from "../StatusChip";
import { useTranslation } from "react-i18next";

function verdictStatus(v: TelegramVerdict): "ok" | "warning" | "error" {
  if (v === "ok") return "ok";
  if (v === "blocked" || v === "error") return "error";
  return "warning";
}

function formatBytes(n: number): string {
  if (n >= 1024 * 1024) return `${(n / (1024 * 1024)).toFixed(1)} MB`;
  if (n >= 1024) return `${(n / 1024).toFixed(0)} KB`;
  return `${n} B`;
}

function DetailRow({
  label,
  value,
  mono,
}: Readonly<{ label: string; value: React.ReactNode; mono?: boolean }>) {
  return (
    <Stack direction="row" spacing={2} alignItems="center">
      <Typography
        variant="caption"
        sx={{
          color: colors.text.secondary,
          minWidth: 80,
          textTransform: "uppercase",
          letterSpacing: "0.5px",
        }}
      >
        {label}
      </Typography>
      <Typography
        variant="body2"
        sx={{
          color: colors.text.primary,
          fontFamily: mono ? "monospace" : "inherit",
          fontSize: mono ? "0.8rem" : undefined,
        }}
      >
        {value}
      </Typography>
    </Stack>
  );
}

function ThroughputCard({
  index,
  title,
  icon,
  tp,
}: Readonly<{
  index: number;
  title: string;
  icon: React.ReactNode;
  tp: TelegramThroughput;
}>) {
  const { t } = useTranslation();
  return (
    <Box sx={{ flex: "1 1 300px", minWidth: 0 }}>
      <ResultCard
        index={index}
        status={verdictStatus(tp.verdict)}
        title={
          <Stack direction="row" spacing={1} alignItems="center">
            {icon}
            <span>{title}</span>
          </Stack>
        }
        subtitle={`${tp.mbps_avg} Mbps · ${formatBytes(tp.bytes)} / ${formatBytes(tp.expected)}`}
        badge={<StatusChip status={tp.verdict} />}
        expandedContent={
          <Stack spacing={1} sx={{ py: 0.5 }}>
            <DetailRow label={t("detector.labels.avgSpeed")} value={`${tp.mbps_avg} Mbps`} mono />
            <DetailRow label={t("detector.labels.peakSpeed")} value={`${tp.mbps_peak} Mbps`} mono />
            <DetailRow
              label={t("detector.labels.transferred")}
              value={`${formatBytes(tp.bytes)} / ${formatBytes(tp.expected)} (${tp.pct_ok}%)`}
              mono
            />
            <DetailRow label={t("detector.labels.duration")} value={`${(tp.duration_ms / 1000).toFixed(1)} s`} mono />
            {tp.detail && (
              <DetailRow
                label={t("detector.labels.detail")}
                value={
                  <Typography variant="caption" sx={{ color: colors.text.secondary }}>
                    {tp.detail}
                  </Typography>
                }
              />
            )}
          </Stack>
        }
      />
    </Box>
  );
}

export function TelegramResults({
  result,
}: Readonly<{ result: TelegramResult }>) {
  const { t } = useTranslation();

  return (
    <Stack spacing={1.5}>
      <Box sx={{ display: "flex", flexWrap: "wrap", gap: 1 }}>
        <ThroughputCard
          index={0}
          title={t("detector.labels.download")}
          icon={<DownloadIcon sx={{ fontSize: 18, color: colors.text.secondary }} />}
          tp={result.download}
        />
      </Box>

      <Box>
        <Typography
          variant="caption"
          sx={{
            color: colors.text.secondary,
            textTransform: "uppercase",
            letterSpacing: "0.5px",
          }}
        >
          {t("detector.labels.datacenters")} · {result.dc_reachable}/{result.dc_total}
        </Typography>
        <Box sx={{ display: "flex", flexWrap: "wrap", gap: 1, mt: 0.5 }}>
          {result.dc_pings.map((p) => (
            <B4Badge
              key={p.dc}
              label={`DC${p.dc}${p.ok && p.rtt_ms != null ? ` · ${p.rtt_ms} ms` : ""}`}
              size="small"
              color={p.ok ? "primary" : "error"}
            />
          ))}
        </Box>
      </Box>
    </Stack>
  );
}
