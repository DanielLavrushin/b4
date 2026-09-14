import type { CSSProperties } from "react";
import { createTheme } from "@mui/material";
import {
  colors,
  fonts,
  gradients,
  glows,
  radiusPx,
  typography as tokens,
} from "./tokens";

declare module "@mui/material/styles" {
  interface TypographyVariants {
    metricLabel: CSSProperties;
    displayMetric: CSSProperties;
    sectionHeader: CSSProperties;
    monoSmall: CSSProperties;
  }
  interface TypographyVariantsOptions {
    metricLabel?: CSSProperties;
    displayMetric?: CSSProperties;
    sectionHeader?: CSSProperties;
    monoSmall?: CSSProperties;
  }
  interface Palette {
    glow: { primary: string; amber: string };
    gradient: {
      appBar: string;
      logo: string;
      scrollbar: string;
      scrollbarHover: string;
      vignette: string;
    };
  }
  interface PaletteOptions {
    glow?: { primary: string; amber: string };
    gradient?: {
      appBar: string;
      logo: string;
      scrollbar: string;
      scrollbarHover: string;
      vignette: string;
    };
  }
}

declare module "@mui/material/Typography" {
  interface TypographyPropsVariantOverrides {
    metricLabel: true;
    displayMetric: true;
    sectionHeader: true;
    monoSmall: true;
  }
}

export const theme = createTheme({
  palette: {
    mode: "dark",
    primary: { main: colors.primary },
    secondary: { main: colors.secondary },
    info: { main: colors.state.info },
    success: { main: colors.state.success },
    warning: { main: colors.state.warning },
    error: { main: colors.state.error },
    background: {
      default: colors.background.default,
      paper: colors.background.paper,
    },
    text: {
      primary: colors.text.primary,
      secondary: colors.text.secondary,
    },
    glow: glows,
    gradient: gradients,
  },
  components: {
    MuiCssBaseline: {
      styleOverrides: {
        body: {
          "*::-webkit-scrollbar": {
            width: "12px",
            height: "12px",
          },
          "*::-webkit-scrollbar-track": {
            background: colors.background.default,
            borderRadius: "6px",
          },
          "*::-webkit-scrollbar-thumb": {
            background: gradients.scrollbar,
            borderRadius: "6px",
            border: `2px solid ${colors.background.default}`,
            "&:hover": {
              background: gradients.scrollbarHover,
            },
          },
          "*::-webkit-scrollbar-thumb:active": {
            background: colors.secondary,
          },
          "*": {
            scrollbarWidth: "thin",
            scrollbarColor: `${colors.primary} ${colors.background.default}`,
          },
        },
        a: {
          color: colors.secondary,
          textDecoration: "none",
          "&:hover": {
            textDecoration: "none",
            color: colors.primary,
          },
        },
      },
    },
    MuiChip: {
      styleOverrides: {
        root: {
          fontWeight: 600,
          fontSize: 12,
          borderRadius: 9999,
          backgroundColor: colors.accent.secondaryHover,
          color: colors.text.primary,
        },
        sizeSmall: {
          height: 24,
        },
        label: {
          paddingLeft: 10,
          paddingRight: 10,
        },
        labelSmall: {
          paddingLeft: 10,
          paddingRight: 10,
        },
        filledPrimary: {
          backgroundColor: colors.primary,
          color: colors.text.primary,
        },
        filledSecondary: {
          backgroundColor: colors.secondary,
          color: colors.text.tertiary,
        },
        outlinedPrimary: {
          borderColor: colors.primary,
          backgroundColor: colors.background.paper,
          color: colors.text.primary,
        },
        outlinedSecondary: {
          borderColor: colors.secondary,
          backgroundColor: colors.accent.secondaryHover,
          color: colors.text.secondary,
        },
        colorInfo: {
          "&.MuiChip-filled": {
            backgroundColor: colors.state.info,
            color: colors.text.tertiary,
          },
          "&.MuiChip-outlined": {
            borderColor: colors.state.info,
            backgroundColor: colors.accent.secondaryHover,
            color: colors.state.info,
          },
        },
        colorSuccess: {
          "&.MuiChip-filled": {
            backgroundColor: colors.state.success,
            color: colors.text.tertiary,
          },
          "&.MuiChip-outlined": {
            borderColor: colors.state.success,
            backgroundColor: colors.accent.secondaryHover,
            color: colors.state.success,
          },
        },
        colorWarning: {
          "&.MuiChip-filled": {
            backgroundColor: colors.state.warning,
            color: colors.text.tertiary,
          },
          "&.MuiChip-outlined": {
            borderColor: colors.state.warning,
            backgroundColor: colors.accent.secondaryHover,
            color: colors.state.warning,
          },
        },
        colorError: {
          "&.MuiChip-filled": {
            backgroundColor: colors.state.error,
            color: colors.text.primary,
          },
          "&.MuiChip-outlined": {
            borderColor: colors.state.error,
            backgroundColor: colors.accent.secondaryHover,
            color: colors.state.error,
          },
        },
      },
    },
    MuiButton: {
      defaultProps: {
        disableElevation: true,
      },
      styleOverrides: {
        root: {
          borderRadius: radiusPx.sm,
          textTransform: "none",
          fontSize: 13,
          fontWeight: 500,
          letterSpacing: "0.02em",
          padding: "6px 16px",
          transition: "all 150ms ease",
          "&:disabled": {
            color: colors.text.disabled,
          },
        },
        textPrimary: {
          color: colors.text.primary,
          backgroundColor: "transparent",
          "&:hover": {
            backgroundColor: colors.accent.primaryHover,
          },
          "&:disabled": {
            color: colors.text.disabled,
          },
        },
        textSecondary: {
          color: colors.secondary,
          backgroundColor: "transparent",
          "&:hover": {
            backgroundColor: colors.accent.secondaryHover,
            color: colors.secondary,
          },
          "&:disabled": {
            color: colors.text.disabled,
          },
        },
        containedPrimary: {
          backgroundColor: colors.primary,
          color: colors.text.primary,
          "&:hover": {
            backgroundColor: colors.secondary,
            color: colors.text.tertiary,
          },
          "&:disabled": {
            backgroundColor: colors.accent.primary,
            color: colors.text.disabled,
          },
        },
        containedSecondary: {
          backgroundColor: colors.secondary,
          color: colors.text.tertiary,
          "&:hover": {
            backgroundColor: colors.secondary,
            color: colors.text.tertiary,
          },
          "&:disabled": {
            backgroundColor: colors.accent.secondary,
            color: colors.text.disabled,
          },
        },
        outlinedPrimary: {
          borderColor: colors.primary,
          backgroundColor: colors.accent.primaryHover,
          color: colors.text.primary,
          "&:hover": {
            borderColor: colors.secondary,
            backgroundColor: colors.accent.secondaryHover,
            color: colors.text.secondary,
          },
          "&:disabled": {
            borderColor: colors.accent.primary,
            backgroundColor: colors.accent.primary,
            color: colors.text.disabled,
          },
        },
        outlinedSecondary: {
          borderColor: colors.secondary,
          backgroundColor: colors.accent.secondaryHover,
          color: colors.text.secondary,
          "&:hover": {
            borderColor: colors.primary,
            backgroundColor: colors.accent.primaryHover,
            color: colors.text.primary,
          },
          "&:disabled": {
            borderColor: colors.accent.secondary,
            backgroundColor: colors.accent.secondary,
            color: colors.text.disabled,
          },
        },
      },
    },
    MuiAppBar: {
      styleOverrides: {
        root: {
          background: gradients.appBar,
        },
      },
    },
    MuiPaper: {
      styleOverrides: {
        root: {
          backgroundImage: "none",
          borderColor: colors.border.default,
        },
      },
    },
    MuiDrawer: {
      styleOverrides: {
        paper: {
          backgroundColor: colors.background.paper,
          borderRight: `1px solid ${colors.border.default}`,
        },
      },
    },
    MuiAlert: {
      styleOverrides: {
        root: {
          borderRadius: radiusPx.sm,
        },
        standardInfo: {
          backgroundColor: `${colors.accent.primary}`,
          borderLeft: `1px solid ${colors.text.secondary}`,
          color: colors.text.primary,
          "& .MuiAlert-icon": {
            color: colors.text.secondary,
          },
        },
      },
    },
    MuiToggleButtonGroup: {
      styleOverrides: {
        root: {
          border: `1px solid ${colors.border.medium}`,
          borderRadius: radiusPx.sm,
          overflow: "hidden",
        },
        grouped: {
          border: 0,
          "&:not(:last-of-type)": {
            borderRight: `1px solid ${colors.border.light}`,
            borderRadius: 0,
          },
          "&:first-of-type": {
            borderRadius: 0,
          },
          "&:last-of-type": {
            borderRadius: 0,
          },
        },
      },
    },
    MuiToggleButton: {
      styleOverrides: {
        root: {
          padding: "5px 12px",
          fontSize: 11,
          fontWeight: 600,
          letterSpacing: "0.04em",
          textTransform: "uppercase",
          color: colors.text.secondary,
          backgroundColor: "transparent",
          border: 0,
          "&:hover": {
            backgroundColor: colors.accent.secondaryHover,
            color: colors.text.primary,
          },
          "&.Mui-selected": {
            backgroundColor: colors.secondary,
            color: colors.text.tertiary,
            "&:hover": {
              backgroundColor: colors.secondary,
              color: colors.text.tertiary,
            },
          },
          "&.Mui-disabled": {
            color: colors.text.disabled,
            border: 0,
          },
        },
      },
    },
    MuiSwitch: {
      styleOverrides: {
        root: {
          padding: 0,
          overflow: "visible",
        },
        switchBase: {
          transitionDuration: "200ms",
          "&.Mui-checked": {
            "& .MuiSwitch-thumb": {
              backgroundColor: colors.secondary,
            },
            "& + .MuiSwitch-track": {
              backgroundColor: colors.border.strong,
              opacity: 1,
              border: 0,
            },
          },
          "&.Mui-disabled": {
            opacity: 0.5,
            "& + .MuiSwitch-track": {
              opacity: 1,
            },
          },
        },
        thumb: {
          boxSizing: "border-box",
          backgroundColor: colors.control.thumbOff,
          boxShadow: "none",
        },
        track: {
          backgroundColor: colors.control.trackOff,
          opacity: 1,
          border: 0,
        },
        sizeMedium: {
          width: 42,
          height: 24,
          "& .MuiSwitch-switchBase": {
            padding: 3,
            "&.Mui-checked": {
              transform: "translateX(18px)",
            },
          },
          "& .MuiSwitch-thumb": {
            width: 18,
            height: 18,
          },
          "& .MuiSwitch-track": {
            borderRadius: 12,
          },
        },
        sizeSmall: {
          width: 28,
          height: 16,
          "& .MuiSwitch-switchBase": {
            padding: 2,
            "&.Mui-checked": {
              transform: "translateX(12px)",
            },
          },
          "& .MuiSwitch-thumb": {
            width: 12,
            height: 12,
          },
          "& .MuiSwitch-track": {
            borderRadius: 8,
          },
        },
      },
    },
  },
  typography: {
    fontFamily: fonts.sans,
    metricLabel: tokens.recipes.metricLabel,
    displayMetric: tokens.recipes.displayMetric,
    sectionHeader: tokens.recipes.sectionHeader,
    monoSmall: tokens.recipes.monoSmall,
  },
});
