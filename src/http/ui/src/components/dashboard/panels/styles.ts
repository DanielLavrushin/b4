import type { SxProps, Theme } from "@mui/material";
import { colors, radiusPx } from "@design";

export const PANEL_PAD_X = "14px";

export const numeric = { fontVariantNumeric: "tabular-nums" } as const;

export const srOnlySx = {
  position: "absolute",
  width: "1px",
  height: "1px",
  margin: "-1px",
  padding: 0,
  overflow: "hidden",
  clip: "rect(0 0 0 0)",
  whiteSpace: "nowrap",
  border: 0,
} satisfies SxProps<Theme>;

export const sectionLabelSx = {
  display: "block",
  fontSize: 11,
  fontWeight: 600,
  letterSpacing: "0.12em",
  textTransform: "uppercase",
  color: colors.text.secondary,
  lineHeight: 1.4,
} satisfies SxProps<Theme>;

export const listSx = {
  listStyle: "none",
  m: 0,
  p: 0,
} satisfies SxProps<Theme>;

export const rowSx = {
  px: PANEL_PAD_X,
  py: "8px",
  borderTop: `1px solid ${colors.border.light}`,
} satisfies SxProps<Theme>;

export const emptySx = {
  px: PANEL_PAD_X,
  py: "14px",
  fontSize: 13,
  lineHeight: 1.5,
  color: colors.text.secondary,
} satisfies SxProps<Theme>;

export const tagSx = {
  display: "inline-flex",
  alignItems: "center",
  gap: "4px",
  height: 20,
  px: "7px",
  border: `1px solid ${colors.border.default}`,
  borderRadius: `${radiusPx.lg}px`,
  fontSize: 11,
  fontWeight: 600,
  lineHeight: 1,
  whiteSpace: "nowrap",
  color: colors.text.secondary,
  flexShrink: 0,
} satisfies SxProps<Theme>;

export const linkSx = {
  fontSize: 12,
  fontWeight: 600,
  color: colors.secondary,
  whiteSpace: "nowrap",
  textUnderlineOffset: "3px",
  "&:hover": { color: colors.secondary, textDecoration: "underline" },
  "&:focus-visible": {
    outline: `2px solid ${colors.border.strong}`,
    outlineOffset: "2px",
    borderRadius: "2px",
  },
} satisfies SxProps<Theme>;
