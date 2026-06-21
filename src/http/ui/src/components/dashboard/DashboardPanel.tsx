import { Box, Paper, Typography } from "@mui/material";
import { colors, radiusPx } from "@design";

interface DashboardPanelProps {
  eyebrow?: string;
  icon?: React.ReactNode;
  right?: React.ReactNode;
  padded?: boolean;
  divider?: boolean;
  fill?: boolean;
  children: React.ReactNode;
  sx?: object;
}

export const DashboardPanel = ({
  eyebrow,
  icon,
  right,
  padded = false,
  divider = false,
  fill = false,
  children,
  sx,
}: DashboardPanelProps) => (
  <Paper
    variant="outlined"
    sx={{
      bgcolor: colors.background.paper,
      borderColor: colors.border.default,
      borderRadius: `${radiusPx.md}px`,
      overflow: "hidden",
      display: "flex",
      flexDirection: "column",
      height: fill ? "100%" : undefined,
      ...sx,
    }}
  >
    {(eyebrow || right || icon) && (
      <Box
        sx={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          gap: "12px",
          p: "12px 14px 6px",
          borderBottom: divider ? `1px solid ${colors.border.light}` : undefined,
          ...(divider ? { pb: "12px" } : null),
        }}
      >
        <Box sx={{ display: "flex", alignItems: "center", gap: "8px", minWidth: 0 }}>
          {icon}
          {eyebrow && (
            <Typography
              variant="metricLabel"
              sx={{ color: colors.text.secondary, opacity: 0.8 }}
            >
              {eyebrow}
            </Typography>
          )}
        </Box>
        {right}
      </Box>
    )}
    <Box
      sx={{
        flex: fill ? 1 : undefined,
        minHeight: 0,
        ...(padded ? { p: "0 14px 14px" } : null),
      }}
    >
      {children}
    </Box>
  </Paper>
);
