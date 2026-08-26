import { memo } from "react";
import { Box } from "@mui/material";
import { colors } from "@design";

interface B4MiniBarsProps {
  data: number[];
  color?: string;
  height?: number;
  sx?: object;
}

const BAR_WIDTH = 0.5;

export const B4MiniBars = memo<B4MiniBarsProps>(
  ({ data, color = colors.secondary, height = 24, sx }) => {
    let max = 1;
    for (const v of data) if (v > max) max = v;
    const minBar = Math.max(2, height * 0.08);

    return (
      <Box sx={{ display: "flex", height, flex: 1, minWidth: 0, ...sx }}>
        <svg
          viewBox={`0 0 ${Math.max(1, data.length)} ${height}`}
          preserveAspectRatio="none"
          style={{ display: "block", width: "100%", height }}
        >
          {data.map((v, i) => {
            const h = Math.max(minBar, (v / max) * height);
            return (
              <rect
                key={i}
                x={i}
                y={height - h}
                width={BAR_WIDTH}
                height={h}
                fill={color}
              />
            );
          })}
        </svg>
      </Box>
    );
  },
  (prev, next) =>
    prev.color === next.color &&
    prev.height === next.height &&
    prev.sx === next.sx &&
    prev.data.length === next.data.length &&
    prev.data.every((v, i) => v === next.data[i]),
);

B4MiniBars.displayName = "B4MiniBars";
