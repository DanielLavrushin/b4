import {
  Accordion,
  AccordionDetails,
  AccordionSummary,
  Box,
  Chip,
  Typography,
} from "@mui/material";
import ExpandMoreIcon from "@mui/icons-material/ExpandMore";
import { colors } from "@design";
import type { ReactNode } from "react";
import { useState } from "react";

interface B4AccordionProps {
  title: string;
  status?: string;
  enabled?: boolean;
  defaultExpanded?: boolean;
  expanded?: boolean;
  onChange?: (expanded: boolean) => void;
  hasChanges?: boolean;
  children: ReactNode;
}

export const B4Accordion = ({
  title,
  status,
  enabled,
  defaultExpanded,
  expanded,
  onChange,
  hasChanges,
  children,
}: B4AccordionProps) => {
  const isControlled = expanded !== undefined;
  const [, setInnerExpanded] = useState(!!defaultExpanded);
  const [settledCollapsed, setSettledCollapsed] = useState(
    isControlled ? expanded === false : !defaultExpanded,
  );
  const handleChange = (_: unknown, next: boolean) => {
    if (!isControlled) setInnerExpanded(next);
    if (next) setSettledCollapsed(false);
    onChange?.(next);
  };
  return (
    <Accordion
      defaultExpanded={isControlled ? undefined : defaultExpanded}
      expanded={isControlled ? expanded : undefined}
      onChange={handleChange}
      TransitionProps={{
        onExited: () => setSettledCollapsed(true),
      }}
      disableGutters
      sx={{
        bgcolor: "transparent",
        border: `1px solid ${colors.border.light}`,
        borderRadius: "8px !important",
        "&::before": { display: "none" },
        "&.Mui-expanded": {
          borderColor: colors.border.medium,
        },
        ...(hasChanges && settledCollapsed
          ? { boxShadow: `inset 3px 0 0 ${colors.secondary}` }
          : {}),
      }}
    >
      <AccordionSummary
        expandIcon={<ExpandMoreIcon sx={{ color: colors.text.secondary }} />}
        sx={{
          px: 2,
          py: 0.5,
          minHeight: 48,
          "&.Mui-expanded": { minHeight: 48 },
          "& .MuiAccordionSummary-content": {
            my: 1,
            alignItems: "center",
            gap: 1.5,
          },
        }}
      >
        <Typography
          variant="subtitle2"
          sx={{
            color: colors.text.primary,
            fontWeight: 600,
            textTransform: "uppercase",
            fontSize: "0.75rem",
            letterSpacing: "0.05em",
          }}
        >
          {title}
        </Typography>
        {status && (
          <Chip
            label={status}
            size="small"
            sx={{
              height: 20,
              fontSize: "0.65rem",
              fontWeight: 600,
              bgcolor: enabled
                ? colors.accent.primary
                : "rgba(255,255,255,0.05)",
              color: enabled ? colors.primary : colors.text.disabled,
              border: `1px solid ${enabled ? colors.primary : colors.border.light}`,
            }}
          />
        )}
      </AccordionSummary>
      <AccordionDetails sx={{ px: 2, pt: 0, pb: 2 }}>
        <Box sx={{ display: "flex", flexDirection: "column", gap: 2 }}>
          {children}
        </Box>
      </AccordionDetails>
    </Accordion>
  );
};
