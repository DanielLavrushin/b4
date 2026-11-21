import { Grid, Box, Typography } from "@mui/material";
import { colors } from "@design";
import { B4SetConfig } from "@models/config";
import { B4FormHeader } from "@b4.elements";
import { firstByteWait } from "@utils";
import { useTranslation } from "react-i18next";

interface FirstByteSettingsProps {
  config: B4SetConfig;
}

export const FirstByteSettings = ({ config }: FirstByteSettingsProps) => {
  const { t } = useTranslation();
  const wait = firstByteWait(
    config.tcp.seg2delay,
    config.tcp.seg2delay_max,
    t("core.units.ms"),
  );

  return (
    <>
      <B4FormHeader label={t("sets.tcp.splitting.firstByte.header")} />

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
              <Typography variant="caption">⏱️ {wait}</Typography>
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
            color="text.secondary"
            sx={{ mt: 1, display: "block" }}
          >
            {t("sets.tcp.splitting.firstByte.vizNote")}
          </Typography>
        </Box>
      </Grid>
    </>
  );
};
