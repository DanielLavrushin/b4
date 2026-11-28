import { colors, radius } from "../tokens";

export const GROUP_ROW_GAP = 44;
const TRAY_TOP = -30;
const TRAY_BOTTOM = -6;
const TRAY_END = -8;
const TRAY_JOIN = -12;
const TRAY_STACK = -GROUP_ROW_GAP / 2;

export const groupTraySx = (first: boolean, last: boolean) => ({
  "&::before": {
    content: '""',
    position: "absolute",
    zIndex: -1,
    pointerEvents: "none",
    top: { xs: first ? TRAY_TOP : TRAY_STACK, sm: TRAY_TOP },
    bottom: { xs: last ? TRAY_BOTTOM : TRAY_STACK, sm: TRAY_BOTTOM },
    left: { xs: TRAY_END, sm: first ? TRAY_END : TRAY_JOIN },
    right: { xs: TRAY_END, sm: last ? TRAY_END : TRAY_JOIN },
    bgcolor: colors.background.dark,
    borderStyle: "solid",
    borderColor: colors.border.default,
    borderLeftColor: { xs: colors.secondary, sm: first ? colors.secondary : colors.border.default },
    borderTopWidth: { xs: first ? 1 : 0, sm: 1 },
    borderBottomWidth: { xs: last ? 1 : 0, sm: 1 },
    borderLeftWidth: { xs: 3, sm: first ? 3 : 0 },
    borderRightWidth: { xs: 1, sm: last ? 1 : 0 },
    borderTopLeftRadius: first ? radius.md : 0,
    borderTopRightRadius: { xs: first ? radius.md : 0, sm: last ? radius.md : 0 },
    borderBottomLeftRadius: { xs: last ? radius.md : 0, sm: first ? radius.md : 0 },
    borderBottomRightRadius: last ? radius.md : 0,
  },
});
