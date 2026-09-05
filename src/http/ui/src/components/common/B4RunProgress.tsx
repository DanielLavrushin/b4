import { ReactNode } from "react";
import { Box, Button, CircularProgress, LinearProgress, Stack, Typography } from "@mui/material";
import { StopIcon } from "@b4.icons";
import { colors, typography } from "@design";

interface B4RunHeaderProps {
  title: string;
  subtitle?: ReactNode;
  onStop?: () => void;
  stopping?: boolean;
  stopLabel: string;
  stoppingLabel: string;
}

export const B4RunHeader = ({ title, subtitle, onStop, stopping = false, stopLabel, stoppingLabel }: B4RunHeaderProps) => (
  <Stack direction="row" justifyContent="space-between" alignItems="flex-start" flexWrap="wrap" useFlexGap spacing={2}>
    <Box>
      <Typography sx={{ fontSize: 18, fontWeight: 600 }}>{title}</Typography>
      {subtitle && (
        <Typography variant="caption" sx={{ color: colors.text.secondary }}>
          {subtitle}
        </Typography>
      )}
    </Box>
    {onStop && (
      <Button
        variant="outlined"
        color="secondary"
        startIcon={stopping ? <CircularProgress size={16} color="inherit" /> : <StopIcon />}
        onClick={onStop}
        disabled={stopping}
        sx={{ whiteSpace: "nowrap" }}
      >
        {stopping ? stoppingLabel : stopLabel}
      </Button>
    )}
  </Stack>
);

export interface B4RunStep {
  key: string;
  label: string;
  note?: string;
  skipped?: boolean;
}

interface B4RunStepsProps {
  steps: B4RunStep[];
  active: number;
}

export const B4RunSteps = ({ steps, active }: B4RunStepsProps) => (
  <Box sx={{ display: "flex", alignItems: "center", flexWrap: "wrap", rowGap: 1 }}>
    {steps.map((step, i) => {
      const done = i < active;
      const on = i === active;
      const color = on ? colors.secondary : done ? colors.text.secondary : colors.text.disabled;
      return (
        <Box key={step.key} sx={{ display: "flex", alignItems: "center", flex: "0 1 auto" }}>
          <Box sx={{ display: "flex", alignItems: "center", gap: 1, color, fontSize: 12, fontWeight: on ? 600 : 400, whiteSpace: "nowrap" }}>
            <Box
              sx={{
                width: 22,
                height: 22,
                borderRadius: "50%",
                display: "grid",
                placeItems: "center",
                fontSize: 11,
                border: `1px solid ${on ? colors.secondary : done ? colors.primary : colors.border.default}`,
                bgcolor: on ? colors.secondary : done ? colors.accent.primary : "transparent",
                color: on ? colors.background.dark : done ? colors.primaryLight : colors.text.disabled,
              }}
            >
              {done && !step.skipped ? "✓" : i + 1}
            </Box>
            {step.label}
            {step.note && (
              <Box component="span" sx={{ fontWeight: 400, color: on ? undefined : colors.text.disabled }}>
                · {step.note}
              </Box>
            )}
          </Box>
          {i < steps.length - 1 && <Box sx={{ width: 22, height: 1, mx: 1.25, bgcolor: done ? colors.primary : colors.border.light }} />}
        </Box>
      );
    })}
  </Box>
);

interface B4RunBarProps {
  value: number;
  indeterminate?: boolean;
}

export const B4RunBar = ({ value, indeterminate = false }: B4RunBarProps) => (
  <LinearProgress
    variant={indeterminate ? "indeterminate" : "determinate"}
    value={Math.min(100, Math.max(0, value))}
    sx={{
      height: 4,
      borderRadius: 2,
      bgcolor: colors.background.dark,
      "& .MuiLinearProgress-bar": { bgcolor: colors.secondary },
    }}
  />
);

interface B4RunLineProps {
  text: string;
  placeholder?: string;
  live?: boolean;
  action?: ReactNode;
}

export const B4RunLine = ({ text, placeholder = "", live = true, action }: B4RunLineProps) => (
  <Box
    sx={{
      display: "flex",
      alignItems: "center",
      gap: 1.5,
      px: 1.5,
      py: 1,
      border: `1px solid ${colors.border.light}`,
      borderRadius: 1.5,
      bgcolor: colors.background.dark,
      minWidth: 0,
    }}
  >
    <Box
      sx={{
        width: 9,
        height: 9,
        borderRadius: "50%",
        flexShrink: 0,
        bgcolor: live ? colors.secondary : colors.text.disabled,
        boxShadow: live ? `0 0 8px ${colors.secondary}` : "none",
      }}
    />
    <Typography
      noWrap
      sx={{
        ...typography.recipes.monoSmall,
        fontSize: typography.sizes.sm,
        color: text ? colors.text.primary : colors.text.disabled,
        flex: 1,
        minWidth: 0,
      }}
    >
      {text || placeholder}
    </Typography>
    {action}
  </Box>
);
