import { Box, Divider, Paper, Switch, Typography } from "@mui/material";
import { colors, radiusPx } from "@design";
import { useChangedField } from "@context/ChangedFieldsContext";
import { B4Help, type B4HelpContent } from "./B4Help";

export interface B4IntegrationCardProps extends B4HelpContent {
  icon: React.ReactNode;
  title: string;
  description?: string;
  status?: React.ReactNode;
  enabled?: boolean;
  onToggle?: (enabled: boolean) => void;
  toggleLabel?: string;
  togglePath?: string;
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
  togglePath,
  children,
  info,
  learnMore,
  credit,
  aiTopic,
  aiContext,
  aiQuestion,
}: B4IntegrationCardProps) => {
  const open = !onToggle || enabled;
  const changed = useChangedField(togglePath, toggleLabel ?? title);

  return (
    <Paper
      variant="outlined"
      sx={{
        bgcolor: colors.background.paper,
        border: `1px solid ${colors.border.default}`,
        ...(changed
          ? { boxShadow: `inset 3px 0 0 ${colors.secondary}` }
          : {}),
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
          <Box sx={{ display: "flex", alignItems: "center", gap: 0.5 }}>
            <Typography
              sx={{
                fontSize: 17,
                fontWeight: 600,
                lineHeight: 1.3,
                color: changed ? colors.secondary : colors.text.primary,
              }}
            >
              {title}
            </Typography>
            <B4Help
              label={title}
              info={info}
              learnMore={learnMore}
              credit={credit}
              aiTopic={aiTopic}
              aiContext={aiContext}
              aiQuestion={aiQuestion}
              fieldDoc={description}
              value={onToggle ? enabled : undefined}
            />
          </Box>
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

