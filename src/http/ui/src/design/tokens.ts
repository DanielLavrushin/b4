export const colors = {
  primary: "#9E1C60",
  secondary: "#F5AD18",
  tertiary: "#811844",
  quaternary: "#561530",
  background: {
    default: "#1a0e15",
    paper: "#1f1218",
    dark: "#0f0a0e",
    control: "rgba(31, 18, 24, 0.6)",
    hover: "rgba(255, 255, 255, 0.025)",
  },
  text: {
    primary: "#ffe8f4",
    secondary: "#f8d7e9",
    disabled: "rgba(255, 232, 244, 0.5)",
    tertiary: "#1a0e15",
  },
  border: {
    default: "rgba(245, 173, 24, 0.24)",
    light: "rgba(245, 173, 24, 0.12)",
    medium: "rgba(245, 173, 24, 0.24)",
    strong: "rgba(245, 173, 24, 0.5)",
  },
  accent: {
    primary: "rgba(158, 28, 96, 0.2)",
    primaryHover: "rgba(158, 28, 96, 0.3)",
    primaryStrong: "rgba(158, 28, 96, 0.1)",
    secondary: "rgba(245, 173, 24, 0.2)",
    secondaryHover: "rgba(245, 173, 24, 0.1)",
    tertiary: "rgba(129, 24, 68, 0.2)",
  },
  state: {
    success: "#66bb6a",
    info: "#29b6f6",
    warning: "#ffa726",
    error: "#f44336",
  },
  control: {
    trackOff: "rgba(255, 255, 255, 0.18)",
    thumbOff: "#bdbdbd",
  },
} as const;

export const gradients = {
  appBar: `linear-gradient(90deg, ${colors.quaternary} 0%, ${colors.tertiary} 35%, ${colors.primary} 70%, ${colors.secondary} 100%)`,
  logo: `linear-gradient(135deg, ${colors.secondary} 0%, ${colors.primary} 100%)`,
  scrollbar: `linear-gradient(180deg, ${colors.primary} 0%, ${colors.tertiary} 50%, ${colors.quaternary} 100%)`,
  scrollbarHover: `linear-gradient(180deg, ${colors.secondary} 0%, ${colors.primary} 50%, ${colors.tertiary} 100%)`,
  vignette:
    "radial-gradient(ellipse at 50% 50%, rgba(158, 28, 96, 0.3) 0%, transparent 70%)",
} as const;

export const glows = {
  primary: "0 0 20px rgba(158, 28, 96, 0.13)",
  amber: "0 0 24px rgba(245, 173, 24, 0.18)",
} as const;

export const fonts = {
  sans: 'system-ui, -apple-system, "Segoe UI", Roboto, Ubuntu, "Helvetica Neue", Arial, sans-serif',
  mono: '"JetBrains Mono", "SF Mono", "Cascadia Code", Menlo, Consolas, "Roboto Mono", monospace',
} as const;

export const spacing = {
  xs: 0.5,
  sm: 1,
  md: 2,
  lg: 3,
  xl: 4,
  xxl: 6,
} as const;

export const radius = {
  sm: 1,
  md: 2,
  lg: 3,
  xl: 4,
} as const;

export const radiusPx = {
  sm: 4,
  md: 8,
  lg: 12,
  xl: 16,
} as const;

export const typography = {
  sizes: {
    xs: "0.65rem",
    sm: "0.75rem",
    md: "0.875rem",
    lg: "1rem",
    xl: "1.25rem",
  },
  weights: {
    regular: 400,
    medium: 500,
    semibold: 600,
    bold: 700,
    black: 800,
  },
  tracking: {
    tight: "-0.08em",
    wide: "0.15em",
    micro: "0.18em",
  },
  recipes: {
    metricLabel: {
      fontSize: "0.625rem",
      fontWeight: 400,
      lineHeight: 1,
      letterSpacing: "0.18em",
      textTransform: "uppercase",
      color: colors.text.secondary,
    },
    displayMetric: {
      fontSize: "2.25rem",
      fontWeight: 700,
      lineHeight: 1,
      letterSpacing: "-0.015em",
    },
    sectionHeader: {
      fontSize: "1rem",
      fontWeight: 600,
      lineHeight: 1.3,
    },
    monoSmall: {
      fontFamily: fonts.mono,
      fontSize: "0.6875rem",
      fontWeight: 400,
      lineHeight: 1.3,
    },
  },
} as const;
