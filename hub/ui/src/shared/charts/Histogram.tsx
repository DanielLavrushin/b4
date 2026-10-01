import { Box, Tooltip, Typography } from "@mui/material";
import { colors } from "@design";
import type { Series } from "./DailyBars";

interface HistogramProps {
  bins: { label: string; values: Record<string, number>; tip?: string }[];
  series: Series[];
  height?: number;
}

export function Histogram({ bins, series, height = 120 }: Readonly<HistogramProps>) {
  const max = Math.max(1, ...bins.map((b) => series.reduce((sum, s) => sum + (b.values[s.key] ?? 0), 0)));
  return (
    <Box>
      <Box sx={{ display: "flex", alignItems: "flex-end", gap: 0.5, height, borderBottom: `1px solid ${colors.border.default}` }}>
        {bins.map((b) => {
          const total = series.reduce((sum, s) => sum + (b.values[s.key] ?? 0), 0);
          return (
            <Tooltip key={b.label} title={b.tip ?? `${b.label}: ${series.map((s) => `${s.label} ${String(b.values[s.key] ?? 0)}`).join(", ")}`}>
              <Box sx={{ flex: 1, height: "100%", display: "flex", flexDirection: "column", justifyContent: "flex-end", alignItems: "stretch" }}>
                {total > 0 && (
                  <Typography variant="caption" sx={{ textAlign: "center", color: colors.text.secondary, lineHeight: 1.2 }}>
                    {total}
                  </Typography>
                )}
                {[...series].reverse().map((s) => {
                  const v = b.values[s.key] ?? 0;
                  if (v === 0) return null;
                  return <Box key={s.key} sx={{ height: `${String((v / max) * (height - 20))}px`, minHeight: 2, bgcolor: s.color, borderRadius: 0.5 }} />;
                })}
              </Box>
            </Tooltip>
          );
        })}
      </Box>
      <Box sx={{ display: "flex", gap: 0.5, mt: 0.5 }}>
        {bins.map((b) => (
          <Typography key={b.label} variant="caption" sx={{ flex: 1, textAlign: "center", color: colors.text.disabled, fontSize: 10 }}>
            {b.label}
          </Typography>
        ))}
      </Box>
    </Box>
  );
}
