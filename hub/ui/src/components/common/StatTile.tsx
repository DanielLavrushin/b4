import { Box, Paper, Typography } from "@mui/material";
import type { ReactNode } from "react";
import { colors, radiusPx } from "@design";

interface StatTileProps {
  label: string;
  value: ReactNode;
  hint?: string;
  accent?: string;
  onClick?: () => void;
}

export function StatTile({ label, value, hint, accent = colors.secondary, onClick }: StatTileProps) {
  return (
    <Paper
      variant="outlined"
      onClick={onClick}
      sx={{
        p: "18px 20px",
        bgcolor: colors.background.paper,
        border: `1px solid ${colors.border.default}`,
        borderRadius: `${radiusPx.md}px`,
        position: "relative",
        overflow: "hidden",
        cursor: onClick ? "pointer" : "default",
        transition: "border-color .15s",
        "&:hover": onClick ? { borderColor: colors.border.strong } : undefined,
        "&::before": {
          content: '""',
          position: "absolute",
          left: 0,
          top: 0,
          bottom: 0,
          width: 3,
          background: accent,
        },
      }}
    >
      <Typography variant="metricLabel" sx={{ display: "block" }}>
        {label}
      </Typography>
      <Typography variant="displayMetric" sx={{ mt: "10px", color: colors.text.primary }}>
        {value}
      </Typography>
      {hint && (
        <Box sx={{ mt: "8px" }}>
          <Typography variant="caption" sx={{ color: colors.text.secondary }}>
            {hint}
          </Typography>
        </Box>
      )}
    </Paper>
  );
}
