import { Box, Divider, Paper, Typography } from "@mui/material";
import type { ReactNode } from "react";
import { colors, radiusPx } from "@design";

interface SectionProps {
  title: string;
  description?: string;
  icon?: ReactNode;
  action?: ReactNode;
  children: ReactNode;
  dense?: boolean;
}

export function Section({ title, description, icon, action, children, dense }: SectionProps) {
  return (
    <Paper
      variant="outlined"
      sx={{
        p: dense ? "16px" : "24px",
        bgcolor: colors.background.paper,
        border: `1px solid ${colors.border.default}`,
        borderRadius: `${radiusPx.md}px`,
        display: "flex",
        flexDirection: "column",
        height: "100%",
        minWidth: 0,
      }}
    >
      <Box sx={{ display: "flex", alignItems: "center", mb: "12px", gap: "12px" }}>
        {icon && (
          <Box
            sx={{
              p: "10px",
              borderRadius: `${radiusPx.md}px`,
              bgcolor: colors.accent.primary,
              color: colors.primaryLight,
              display: "flex",
              alignItems: "center",
            }}
          >
            {icon}
          </Box>
        )}
        <Box sx={{ flex: 1, minWidth: 0 }}>
          <Typography sx={{ fontSize: 18, fontWeight: 600, lineHeight: 1.3, color: colors.text.primary }}>
            {title}
          </Typography>
          {description && (
            <Typography variant="caption" sx={{ color: colors.text.secondary, display: "block", mt: "2px" }}>
              {description}
            </Typography>
          )}
        </Box>
        {action}
      </Box>
      <Divider sx={{ mb: "16px", borderColor: colors.border.light }} />
      <Box sx={{ display: "flex", flexDirection: "column", gap: 2, minWidth: 0 }}>{children}</Box>
    </Paper>
  );
}
