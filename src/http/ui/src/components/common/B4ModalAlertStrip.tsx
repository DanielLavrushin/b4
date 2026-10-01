import { Box, Stack, Typography } from "@mui/material";
import { colors, radiusPx } from "@design";

type Tone = "primary" | "amber" | "danger";

interface B4ModalAlertStripProps {
  icon?: React.ReactNode;
  tone?: Tone;
  meta?: React.ReactNode;
  children: React.ReactNode;
  sx?: object;
}

const tones: Record<Tone, { tile: string; glyph: string }> = {
  primary: { tile: colors.accent.primary, glyph: colors.primary },
  amber: { tile: colors.accent.secondary, glyph: colors.secondary },
  danger: { tile: "rgba(244, 67, 54, 0.18)", glyph: colors.state.error },
};

export const B4ModalAlertStrip = ({
  icon,
  tone = "primary",
  meta,
  children,
  sx,
}: B4ModalAlertStripProps) => {
  const palette = tones[tone];
  return (
    <Stack
      direction="row"
      sx={{
        alignItems: "flex-start",
        gap: "12px",
        p: "12px 14px",
        borderRadius: `${radiusPx.sm}px`,
        bgcolor: colors.background.paper,
        border: `1px solid ${colors.border.default}`,
        ...sx
      }}>
      <Box
        sx={{
          width: 26,
          height: 26,
          borderRadius: "50%",
          bgcolor: palette.tile,
          color: palette.glyph,
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          flexShrink: 0,
          mt: "1px",
        }}
      >
        {icon ?? <Box sx={{ fontWeight: 700, fontSize: 14 }}>i</Box>}
      </Box>
      <Box sx={{ flex: 1, minWidth: 0 }}>
        <Typography
          component="div"
          sx={{
            fontSize: 12.5,
            lineHeight: 1.5,
            color: colors.text.primary,
          }}
        >
          {children}
        </Typography>
        {meta && (
          <Typography
            component="div"
            sx={{
              fontSize: 11,
              color: colors.text.secondary,
              mt: "4px",
            }}
          >
            {meta}
          </Typography>
        )}
      </Box>
    </Stack>
  );
};
