import { Box, Stack, Typography } from "@mui/material";
import { colors } from "@design";
import { B4Badge } from "@b4.elements";
import type { DNSAvailProviderResult } from "@models/detector";
import { ResultCard } from "../ResultCard";
import { useTranslation } from "react-i18next";

function DetailRow({
  label,
  value,
  mono,
}: Readonly<{
  label: string;
  value: React.ReactNode;
  mono?: boolean;
}>) {
  return (
    <Stack direction="row" spacing={2} alignItems="center">
      <Typography
        variant="caption"
        sx={{
          color: colors.text.secondary,
          minWidth: 70,
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

export function DNSAvailabilityResults({
  providers,
}: Readonly<{ providers: DNSAvailProviderResult[] }>) {
  const { t } = useTranslation();

  return (
    <Box sx={{ display: "flex", flexWrap: "wrap", gap: 1 }}>
      {providers.map((p, index) => {
        const status = p.ok ? "ok" : "error";
        const value = p.ok ? `${p.avg_ms} ms` : t("detector.results.dnsAvailTimeout");

        return (
          <Box
            key={`${p.kind}-${p.provider}-${p.address}`}
            sx={{ flex: "1 1 280px", minWidth: 0 }}
          >
            <ResultCard
              index={index}
              status={status}
              title={p.provider}
              subtitle={`${p.kind.toUpperCase()} · ${p.address}`}
              badge={
                <Stack direction="row" alignItems="center" spacing={0.5}>
                  <B4Badge
                    label={p.kind.toUpperCase()}
                    size="small"
                    color="info"
                  />
                  <B4Badge
                    label={value}
                    size="small"
                    color={p.ok ? "primary" : "error"}
                  />
                </Stack>
              }
              expandedContent={
                <Stack spacing={1} sx={{ py: 0.5 }}>
                  <DetailRow label={t("detector.labels.address")} value={p.address} mono />
                  <DetailRow label={t("detector.labels.kind")} value={p.kind.toUpperCase()} />
                  <DetailRow label={t("detector.labels.avgLatency")} value={p.ok ? `${p.avg_ms} ms` : "—"} mono />
                  <DetailRow
                    label={t("detector.labels.reachable")}
                    value={`${p.ok_count}/${p.total}`}
                    mono
                  />
                </Stack>
              }
            />
          </Box>
        );
      })}
    </Box>
  );
}
