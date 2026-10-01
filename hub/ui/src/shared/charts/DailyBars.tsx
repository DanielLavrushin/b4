import { useId } from "react";
import { Box, Stack, Typography } from "@mui/material";
import { colors } from "@design";

export interface Series {
  key: string;
  label: string;
  color: string;
}

interface DailyBarsProps {
  label: string;
  days: string[];
  values: Record<string, Record<string, number>>;
  series: Series[];
  height?: number;
  dayLabel?: (day: string) => string;
}

export function DailyBars({ label, days, values, series, height = 90, dayLabel }: Readonly<DailyBarsProps>) {
  const id = useId();
  const totals = days.map((d) => series.reduce((sum, s) => sum + (values[d]?.[s.key] ?? 0), 0));
  const summary = series.map((s) => `${s.label} ${String(days.reduce((sum, d) => sum + (values[d]?.[s.key] ?? 0), 0))}`).join(", ");
  const max = Math.max(1, ...totals);
  const bar = 6;
  const gap = 2;
  const width = days.length * (bar + gap);
  return (
    <Box>
      <Box
        component="svg"
        viewBox={`0 0 ${String(width)} ${String(height)}`}
        preserveAspectRatio="none"
        sx={{ width: "100%", height, display: "block" }}
        role="img"
        aria-labelledby={`${id}-title ${id}-desc`}
      >
        <title id={`${id}-title`}>{label}</title>
        <desc id={`${id}-desc`}>{summary}</desc>
        <line x1={0} y1={height - 0.5} x2={width} y2={height - 0.5} stroke={colors.border.default} strokeWidth={1} />
        {days.map((d, i) => {
          let y = height;
          return (
            <g key={d}>
              <title>{`${dayLabel ? dayLabel(d) : d}: ${series.map((s) => `${s.label} ${String(values[d]?.[s.key] ?? 0)}`).join(", ")}`}</title>
              <rect x={i * (bar + gap)} y={0} width={bar} height={height} fill="transparent" />
              {series.map((s) => {
                const v = values[d]?.[s.key] ?? 0;
                if (v === 0) return null;
                const h = Math.max(1, (v / max) * (height - 4));
                y -= h;
                return <rect key={s.key} x={i * (bar + gap)} y={y} width={bar} height={h} fill={s.color} rx={1} />;
              })}
            </g>
          );
        })}
      </Box>
      <Stack direction="row" spacing={2} useFlexGap flexWrap="wrap" sx={{ mt: 0.75 }}>
        {series.map((s) => (
          <Box key={s.key} sx={{ display: "flex", alignItems: "center", gap: 0.5 }}>
            <Box sx={{ width: 10, height: 10, borderRadius: 0.5, bgcolor: s.color }} />
            <Typography variant="caption" sx={{ color: colors.text.secondary }}>
              {s.label}
            </Typography>
          </Box>
        ))}
      </Stack>
    </Box>
  );
}

export const lastDays = (count: number, now = new Date()): string[] => {
  const out: string[] = [];
  const end = Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate());
  for (let i = count - 1; i >= 0; i--) {
    out.push(new Date(end - i * 86_400_000).toISOString().slice(0, 10));
  }
  return out;
};
