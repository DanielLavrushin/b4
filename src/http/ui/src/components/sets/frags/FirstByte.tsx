import { Grid, Box, Typography } from "@mui/material";
import { colors } from "@design";
import { B4SetConfig } from "@models/config";
import { B4Alert, B4FormHeader, B4Hint } from "@b4.elements";
import { useTranslation, Trans } from "react-i18next";

interface FirstByteSettingsProps {
  config: B4SetConfig;
}

export const FirstByteSettings = ({ config }: FirstByteSettingsProps) => {
  const { t } = useTranslation();

  return (
    <>
      <B4FormHeader label={t("sets.tcp.splitting.firstByte.header")} />

      <B4Hint>{t("sets.tcp.splitting.firstByte.alert")}</B4Hint>

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
            component="div"
            sx={{
              color: "text.secondary",
              mb: 1
            }}>
            {t("sets.tcp.splitting.firstByte.timingViz")}
          </Typography>
          <Box
            sx={{
              display: "flex",
              alignItems: "center",
              gap: 1,
              fontFamily: "monospace",
              fontSize: "0.75rem",
            }}
          >
            <Box
              sx={{
                p: 1,
                bgcolor: colors.tertiary,
                borderRadius: 0.5,
                minWidth: 40,
                textAlign: "center",
              }}
            >
              0x16
            </Box>
            <Box
              sx={{
                display: "flex",
                flexDirection: "column",
                alignItems: "center",
                color: colors.text.secondary,
              }}
            >
              <Typography variant="caption">
                ⏱️{" "}
                {config.tcp.seg2delay_max > config.tcp.seg2delay
                  ? `${config.tcp.seg2delay || 30}–${config.tcp.seg2delay_max}ms`
                  : `${config.tcp.seg2delay || 30}ms+`}
              </Typography>
              <Box
                sx={{
                  width: 60,
                  height: 2,
                  bgcolor: colors.quaternary,
                  my: 0.5,
                }}
              />
            </Box>
            <Box
              sx={{
                p: 1,
                bgcolor: colors.accent.secondary,
                borderRadius: 0.5,
                flex: 1,
                textAlign: "center",
              }}
            >
              {t("sets.tcp.splitting.firstByte.restOfPayload")}
            </Box>
          </Box>
          <Typography
            variant="caption"
            sx={{
              color: "text.secondary",
              mt: 1,
              display: "block"
            }}>
            {t("sets.tcp.splitting.firstByte.vizNote")}
          </Typography>
        </Box>
      </Grid>

      <Grid size={{ xs: 12 }}>
        <B4Alert severity="success" sx={{ m: 0 }}>
          <Trans i18nKey="sets.tcp.splitting.firstByte.noConfigNeeded" />
        </B4Alert>
      </Grid>
    </>
  );
};
