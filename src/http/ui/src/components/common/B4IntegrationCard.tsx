import { Box, Divider, Paper, Switch, Typography } from "@mui/material";
import { colors, radiusPx } from "@design";

export interface B4IntegrationCardProps {
  icon: React.ReactNode;
  title: string;
  description?: string;
  status?: React.ReactNode;
  enabled?: boolean;
  onToggle?: (enabled: boolean) => void;
  toggleLabel?: string;
  children: React.ReactNode;
}

export const B4IntegrationCard = ({
  icon,
  title,
  description,
  status,
  enabled = true,
  onToggle,
  toggleLabel,
  children,
}: B4IntegrationCardProps) => {
  const open = !onToggle || enabled;

  return (
    <Paper
      variant="outlined"
      sx={{
        bgcolor: colors.background.paper,
        border: `1px solid ${colors.border.default}`,
        overflow: "hidden",
        opacity: open ? 1 : 0.75,
        transition: "opacity 120ms",
      }}
    >
      <Box
        sx={{
          display: "flex",
          alignItems: "center",
          gap: "16px",
          p: "18px 20px",
        }}
      >
        <Box
          sx={{
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
        <Box sx={{ flex: 1, minWidth: 0 }}>
          <Typography
            sx={{
              fontSize: 17,
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
        <Box sx={{ display: "flex", alignItems: "center", gap: 1.5 }}>
          {status}
          {onToggle && (
            <Switch
              checked={enabled}
              onChange={(e) => onToggle(e.target.checked)}
              slotProps={{ input: { "aria-label": toggleLabel ?? title } }}
            />
          )}
        </Box>
      </Box>

      {open && (
        <>
          <Divider sx={{ borderColor: colors.border.light }} />
          <Box
            sx={{
              p: "20px",
              display: "flex",
              flexDirection: "column",
              gap: 2,
            }}
          >
            {children}
          </Box>
        </>
      )}
    </Paper>
  );
};

export default B4IntegrationCard;
