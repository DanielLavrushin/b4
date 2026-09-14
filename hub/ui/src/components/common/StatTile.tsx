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
  const clickable = onClick !== undefined;
  return (
    <Paper
      variant="outlined"
      component={clickable ? "button" : "div"}
      type={clickable ? "button" : undefined}
      onClick={onClick}
      sx={{
        display: "block",
        width: "100%",
        textAlign: "left",
        font: "inherit",
        color: "inherit",
        appearance: "none",
        p: "18px 20px",
        bgcolor: colors.background.paper,
        border: `1px solid ${colors.border.default}`,
        borderRadius: `${radiusPx.md}px`,
        position: "relative",
        overflow: "hidden",
        cursor: clickable ? "pointer" : "default",
        transition: "border-color .15s",
        "&:hover": clickable ? { borderColor: colors.border.strong } : undefined,
        "&:focus-visible": clickable ? { outline: `2px solid ${colors.primary}`, outlineOffset: 2 } : undefined,
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
