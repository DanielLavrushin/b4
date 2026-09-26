import { memo, useMemo } from "react";
import { Box } from "@mui/material";
import { colors } from "@design";

interface SparklineProps {
  data: number[];
  width?: number;
  height?: number;
  color?: string;
}

export const Sparkline = memo<SparklineProps>(({ data, width = 120, height = 24, color }) => {
  const path = useMemo(() => {
    if (data.length === 0) return { d: "", max: 0 };
    let max = 0;
    for (let i = 0; i < data.length; i++) if (data[i] > max) max = data[i];
    if (max === 0) return { d: "", max: 0 };
    const step = width / Math.max(1, data.length - 1);
    let d = "";
    for (let i = 0; i < data.length; i++) {
      const x = i * step;
      const y = height - (data[i] / max) * (height - 2) - 1;
      d += (i === 0 ? "M" : "L") + x.toFixed(1) + " " + y.toFixed(1) + " ";
    }
    return { d: d.trim(), max };
  }, [data, width, height]);

  const stroke = color ?? colors.secondary;

  if (path.max === 0) {
    return (
      <Box
        sx={{
          width: "100%",
          height,
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          color: colors.text.disabled,
          fontSize: 10,
          fontFamily: "monospace",
        }}
      >
        -
      </Box>
    );
  }

  return (
    <svg
      viewBox={`0 0 ${width} ${height}`}
      preserveAspectRatio="none"
      style={{ display: "block", width: "100%", height }}
    >
      <path d={path.d} fill="none" stroke={stroke} strokeWidth={1.2} strokeLinejoin="round" strokeLinecap="round" />
    </svg>
  );
});

Sparkline.displayName = "Sparkline";
