import { Box, Typography } from "@mui/material";
import type { ReactNode } from "react";
import { colors } from "@design";

export interface Fact {
  label: string;
  value: ReactNode;
  mono?: boolean;
}

export function Facts({ items }: { items: Fact[] }) {
  const shown = items.filter((f) => f.value !== null && f.value !== undefined && f.value !== "");
  return (
    <Box
      component="dl"
      sx={{
        display: "grid",
        gridTemplateColumns: { xs: "1fr", sm: "max-content 1fr" },
        columnGap: "16px",
        rowGap: "6px",
        m: 0,
        minWidth: 0,
      }}
    >
      {shown.map((f) => (
        <Box key={f.label} sx={{ display: "contents" }}>
          <Typography
            component="dt"
            variant="metricLabel"
            sx={{ color: colors.text.secondary, alignSelf: "baseline", pt: "3px", whiteSpace: "nowrap" }}
          >
            {f.label}
          </Typography>
          <Typography
            component="dd"
            variant={f.mono ? "monoSmall" : "body2"}
            sx={{ m: 0, minWidth: 0, overflowWrap: "anywhere", color: colors.text.primary }}
          >
            {f.value}
          </Typography>
        </Box>
      ))}
    </Box>
  );
}
