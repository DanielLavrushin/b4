import { Box } from "@mui/material";
import type { ReactNode } from "react";
import { fonts } from "@design";

export function Mono({ children, title }: { children: ReactNode; title?: string }) {
  return (
    <Box component="span" title={title} sx={{ fontFamily: fonts.mono, fontSize: "0.8em", overflowWrap: "anywhere" }}>
      {children}
    </Box>
  );
}
