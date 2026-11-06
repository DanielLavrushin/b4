import { Box, Paper, Typography, Divider } from "@mui/material";
import { colors, radiusPx } from "@design";

interface B4SectionProps {
  title: string;
  description?: string;
  icon?: React.ReactNode;
  action?: React.ReactNode;
  children: React.ReactNode;
}

export const B4Section = ({
  title,
  description,
  icon,
  action,
  children,
}: B4SectionProps) => {
  return (
    <Paper
      sx={{
        p: "24px",
        bgcolor: colors.background.paper,
        border: `1px solid ${colors.border.default}`,
        display: "flex",
        flexDirection: "column",
        height: "100%",
      }}
      variant="outlined"
    >
      <Box
        sx={{
          display: "flex",
          alignItems: "center",
          flexWrap: "wrap",
          gap: "8px 16px",
          mb: "12px",
        }}
      >
        <Box
          sx={{
            display: "flex",
            alignItems: "center",
            flex: "1 1 240px",
            minWidth: 0,
          }}
        >
          {icon && (
            <Box
              sx={{
                mr: "16px",
                p: "12px",
                borderRadius: `${radiusPx.md}px`,
                bgcolor: colors.accent.primary,
                color: colors.primary,
                display: "flex",
                alignItems: "center",
              }}
            >
              {icon}
            </Box>
          )}
          <Box sx={{ flex: 1, minWidth: 0 }}>
            <Typography
              sx={{
                fontSize: 18,
                fontWeight: 600,
                lineHeight: 1.3,
                color: colors.text.primary,
              }}
            >
              {title}
            </Typography>
            {description && (
              <Typography
                variant="caption"
                sx={{
                  color: colors.text.secondary,
                  display: "block",
                  mt: "2px",
                }}
              >
                {description}
              </Typography>
            )}
          </Box>
        </Box>
        {action}
      </Box>
      <Divider sx={{ mb: "16px", borderColor: colors.border.light }} />
      <Box sx={{ display: "flex", flexDirection: "column", gap: 2 }}>
        {children}
      </Box>
    </Paper>
  );
};

export default B4Section;
