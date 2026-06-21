import { Box } from "@mui/material";
import { colors, fonts } from "@design";

interface DomainLabelProps {
  value: string;
  uppercase?: boolean;
}

export const DomainLabel = ({ value, uppercase = true }: DomainLabelProps) => (
  <Box
    component="span"
    title={value}
    sx={{
      fontFamily: fonts.mono,
      fontSize: 11,
      letterSpacing: "0.04em",
      color: colors.text.primary,
      textTransform: uppercase ? "uppercase" : "none",
      overflow: "hidden",
      textOverflow: "ellipsis",
      whiteSpace: "nowrap",
      flex: 1,
      minWidth: 0,
    }}
  >
    {value}
  </Box>
);
