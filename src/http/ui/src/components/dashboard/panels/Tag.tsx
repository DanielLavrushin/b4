import { forwardRef, type ReactNode } from "react";
import { Box } from "@mui/material";
import { Link as RouterLink } from "react-router";
import { colors } from "@design";
import { tagSx } from "./styles";

interface TagProps {
  icon?: ReactNode;
  iconColor?: string;
  to?: string;
  label: string;
}

const linkTagSx = {
  ...tagSx,
  color: colors.text.primary,
  textDecoration: "none",
  "&:hover": {
    bgcolor: colors.accent.secondaryHover,
    borderColor: colors.border.strong,
  },
  "&:focus-visible": {
    outline: `2px solid ${colors.border.strong}`,
    outlineOffset: "2px",
  },
} as const;

export const Tag = forwardRef<HTMLElement, TagProps>(function Tag(
  { icon, iconColor, to, label, ...rest },
  ref,
) {
  const content = (
    <>
      {icon && (
        <Box
          component="span"
          aria-hidden
          sx={{
            display: "inline-flex",
            color: iconColor ?? colors.text.secondary,
            "& svg": { fontSize: 13 },
          }}
        >
          {icon}
        </Box>
      )}
      <span>{label}</span>
    </>
  );
  if (to) {
    return (
      <Box ref={ref} component={RouterLink} to={to} {...rest} sx={linkTagSx}>
        {content}
      </Box>
    );
  }
  return (
    <Box ref={ref} component="span" {...rest} sx={tagSx}>
      {content}
    </Box>
  );
});
