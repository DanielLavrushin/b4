import { Children, isValidElement } from "react";
import { Box, Grid, Typography } from "@mui/material";
import { colors, spacing } from "@design";

interface B4FormGroupProps {
  label: string;
  description?: string;
  icon?: React.ReactNode;
  children: React.ReactNode;
  columns?: 1 | 2;
}

export const B4FormRow = ({ children }: { children: React.ReactNode }) =>
  children;

const isFullRow = (child: React.ReactNode) =>
  isValidElement(child) && child.type === B4FormRow;

export const B4FormGroup = ({
  label,
  description,
  icon,
  children,
  columns = 2,
}: B4FormGroupProps) => (
  <Box>
    <Box sx={{ display: "flex", alignItems: "center", gap: 1, mb: spacing.md }}>
      {icon}
      <Box>
        <Typography variant="h6" sx={{ color: colors.text.primary }}>
          {label}
        </Typography>
        {description && (
          <Typography variant="caption" sx={{ color: colors.text.secondary }}>
            {description}
          </Typography>
        )}
      </Box>
    </Box>

    <Grid container spacing={spacing.md}>
      {Children.map(children, (child) => (
        <Grid size={{ xs: 12, md: isFullRow(child) ? 12 : 12 / columns }}>
          {child}
        </Grid>
      ))}
    </Grid>
  </Box>
);
