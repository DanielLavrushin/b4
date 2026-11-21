import { Grid, Box, Typography } from "@mui/material";
import { colors } from "@design";
import { B4FormHeader } from "@b4.elements";
import { useTranslation } from "react-i18next";

export const ExtSplitSettings = () => {
  const { t } = useTranslation();

  return (
    <>
      <B4FormHeader label={t("sets.tcp.splitting.extSplit.header")} />

      <Grid size={{ xs: 12 }}>
        <Box
          sx={{
            p: 2,
            bgcolor: colors.background.paper,
            borderRadius: 1,
            border: `1px solid ${colors.border.default}`,
          }}
        >
          <Typography
            variant="caption"
            color="text.secondary"
            component="div"
            sx={{ mb: 1 }}
          >
            {t("sets.tcp.splitting.extSplit.structureViz")}
          </Typography>
          <Box
            sx={{
              display: "flex",
              gap: 0.5,
              fontFamily: "monospace",
              fontSize: "0.7rem",
              flexWrap: "wrap",
            }}
          >
            <Box
              sx={{
                p: 1,
                bgcolor: colors.accent.primary,
                borderRadius: 0.5,
              }}
            >
              TLS Header
            </Box>
            <Box
              sx={{
                p: 1,
                bgcolor: colors.accent.primary,
                borderRadius: 0.5,
              }}
            >
              Handshake
            </Box>
            <Box
              sx={{
                p: 1,
                bgcolor: colors.accent.primary,
                borderRadius: 0.5,
              }}
            >
              Ciphers
            </Box>
            <Box
              sx={{
                p: 1,
                bgcolor: colors.accent.secondary,
                borderRadius: 0.5,
              }}
            >
              Ext₁
            </Box>
            <Box
              sx={{
                p: 1,
                bgcolor: colors.accent.secondary,
                borderRadius: 0.5,
              }}
            >
              Ext₂
            </Box>
            <Box
              sx={{
                p: 1,
                bgcolor: colors.tertiary,
                borderRadius: 0.5,
                position: "relative",
              }}
            >
              <Box
                component="span"
                sx={{
                  position: "absolute",
                  left: -2,
                  top: 0,
                  bottom: 0,
                  width: 3,
                  bgcolor: colors.quaternary,
                }}
              />
              SNI: youtube.com
            </Box>
            <Box
              sx={{
                p: 1,
                bgcolor: colors.accent.secondary,
                borderRadius: 0.5,
              }}
            >
              Ext...
            </Box>
          </Box>
          <Typography
            variant="caption"
            color="text.secondary"
            sx={{ mt: 1, display: "block" }}
          >
            {t("sets.tcp.splitting.extSplit.splitNote")}
          </Typography>
        </Box>
      </Grid>
    </>
  );
};
