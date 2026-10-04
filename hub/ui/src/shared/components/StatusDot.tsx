import { Box, Tooltip, Typography } from "@mui/material";
import type { ReactNode } from "react";
import { colors } from "@design";

export type StatusTone = "success" | "warning" | "error" | "info" | "neutral";

export const statusToneColor: Record<StatusTone, string> = {
  success: colors.state.success,
  warning: colors.state.warning,
  error: colors.state.error,
  info: colors.state.info,
  neutral: colors.text.disabled,
};

interface StatusDotProps {
  tone: StatusTone;
  label: ReactNode;
  tooltip?: ReactNode;
  muted?: boolean;
  variant?: "body2" | "caption";
}

export function StatusDot({ tone, label, tooltip, muted = false, variant = "body2" }: Readonly<StatusDotProps>) {
  const size = variant === "caption" ? 7 : 8;
  const content = (
    <Typography
      component="span"
      variant={variant}
      tabIndex={tooltip ? 0 : undefined}
      sx={{
        display: "inline-flex",
        alignItems: "center",
        gap: 0.75,
        maxWidth: "100%",
        minWidth: 0,
        verticalAlign: "middle",
        whiteSpace: "nowrap",
        color: muted ? colors.text.secondary : colors.text.primary,
        borderRadius: 0.5,
        "&:focus-visible": { outline: `1px solid ${colors.secondary}`, outlineOffset: 2 },
      }}
    >
      <Box component="span" aria-hidden sx={{ width: size, height: size, borderRadius: "50%", bgcolor: statusToneColor[tone], flexShrink: 0 }} />
      <Box component="span" sx={{ minWidth: 0, overflow: "hidden", textOverflow: "ellipsis" }}>
        {label}
      </Box>
    </Typography>
  );
  if (!tooltip) return content;
  return (
    <Tooltip title={tooltip} describeChild>
      {content}
    </Tooltip>
  );
}
