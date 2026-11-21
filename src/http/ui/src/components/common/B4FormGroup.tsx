import { Children, isValidElement } from "react";
import { Box, Grid, Typography } from "@mui/material";
import { colors, spacing } from "@design";
import { B4Help, type B4HelpContent } from "./B4Help";

interface B4FormGroupProps extends B4HelpContent {
  label?: string;
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
  info,
  learnMore,
  credit,
  aiTopic,
  aiContext,
  aiQuestion,
}: B4FormGroupProps) => (
  <Box>
    {(label || description) && (
      <Box
        sx={{ display: "flex", alignItems: "center", gap: 1, mb: spacing.md }}
      >
        {icon}
        <Box>
          {label && (
            <Box sx={{ display: "flex", alignItems: "center", gap: 0.5 }}>
              <Typography
                variant="subtitle1"
                sx={{ color: colors.text.primary, fontWeight: 600 }}
              >
                {label}
              </Typography>
              <B4Help
                label={label}
                info={info}
                learnMore={learnMore}
                credit={credit}
                aiTopic={aiTopic}
                aiContext={aiContext}
                aiQuestion={aiQuestion}
                fieldDoc={description}
              />
            </Box>
          )}
          {description && (
            <Typography
              variant="caption"
              sx={{ color: colors.text.secondary, display: "block" }}
            >
              {description}
            </Typography>
          )}
        </Box>
      </Box>
    )}

    <Grid container spacing={spacing.md}>
      {Children.map(children, (child) =>
        child == null ? null : (
          <Grid size={{ xs: 12, md: isFullRow(child) ? 12 : 12 / columns }}>
            {child}
          </Grid>
        ),
      )}
    </Grid>
  </Box>
);
