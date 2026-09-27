import { Box } from "@mui/material";
import { colors, fonts, radiusPx } from "@design";

export function CodeBlock({ value }: { value: unknown }) {
  const text = typeof value === "string" ? value : JSON.stringify(value, null, 2);
  return (
    <Box
      component="pre"
      sx={{
        m: 0,
        p: "12px",
        fontFamily: fonts.mono,
        fontSize: 12,
        lineHeight: 1.5,
        bgcolor: colors.background.dark,
        border: `1px solid ${colors.border.light}`,
        borderRadius: `${radiusPx.sm}px`,
        overflowX: "auto",
        whiteSpace: "pre",
        color: colors.text.secondary,
      }}
    >
      {text}
    </Box>
  );
}
