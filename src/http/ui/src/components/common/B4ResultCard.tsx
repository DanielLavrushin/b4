import { ReactNode } from "react";
import { Box, Collapse, Stack, Typography } from "@mui/material";
import { colors, spacing } from "@design";
import { B4Card } from "./B4Card";

export type B4ResultStatus = "ok" | "warning" | "error" | "neutral";

const stripe: Record<B4ResultStatus, string> = {
  ok: colors.state.success,
  warning: colors.state.warning,
  error: colors.state.error,
  neutral: colors.text.disabled,
};

interface B4ResultCardProps {
  status: B4ResultStatus;
  title: ReactNode;
  subtitle?: ReactNode;
  badge?: ReactNode;
  children?: ReactNode;
  details?: ReactNode;
  expanded?: boolean;
}

export const B4ResultCard = ({
  status,
  title,
  subtitle,
  badge,
  children,
  details,
  expanded = false,
}: B4ResultCardProps) => (
  <B4Card
    variant="outlined"
    sx={{ borderLeft: `3px solid ${stripe[status]}`, overflow: "hidden" }}
  >
    <Box sx={{ px: spacing.md, py: 1.5 }}>
      <Stack
        direction="row"
        alignItems="flex-start"
        justifyContent="space-between"
        spacing={spacing.md}
        useFlexGap
        flexWrap="wrap"
      >
        <Box sx={{ flex: "1 1 260px", minWidth: 0 }}>
          <Typography
            component="div"
            sx={{
              fontWeight: 600,
              fontSize: "0.95rem",
              color: colors.text.primary,
              overflowWrap: "anywhere",
            }}
          >
            {title}
          </Typography>
          {subtitle && (
            <Typography
              component="div"
              variant="body2"
              sx={{ color: colors.text.secondary, mt: "2px" }}
            >
              {subtitle}
            </Typography>
          )}
        </Box>
        {badge && (
          <Box
            sx={{
              flexShrink: 0,
              display: "flex",
              alignItems: "center",
              gap: 1,
              flexWrap: "wrap",
            }}
          >
            {badge}
          </Box>
        )}
      </Stack>
      {children && <Box sx={{ mt: 1.5 }}>{children}</Box>}
    </Box>
    {details && (
      <Collapse in={expanded} unmountOnExit>
        <Box
          sx={{
            px: spacing.md,
            py: 1.5,
            borderTop: `1px solid ${colors.border.light}`,
            bgcolor: colors.background.dark,
          }}
        >
          {details}
        </Box>
      </Collapse>
    )}
  </B4Card>
);
