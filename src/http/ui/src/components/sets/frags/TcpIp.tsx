import { Grid, Box, Typography } from "@mui/material";

import { B4Switch, B4RangeSlider } from "@b4.fields";
import { B4SetConfig } from "@models/config";
import { colors } from "@design";
import { B4Alert } from "@components/common/B4Alert";
import { B4FormHeader } from "@b4.elements";
import { useTranslation } from "react-i18next";

interface TcpIpSettingsProps {
  config: B4SetConfig;
  onChange: (field: string, value: string | boolean | number) => void;
}

export const TcpIpSettings = ({ config, onChange }: TcpIpSettingsProps) => {
  const { t } = useTranslation();

  const getSplitModeDescription = () => {
    if (config.fragmentation.middle_sni) {
      if (config.fragmentation.sni_position > 0) {
        return t("sets.tcp.splitting.tcpIp.splitMode3seg");
      }
      return t("sets.tcp.splitting.tcpIp.splitModeSni");
    }
    return t("sets.tcp.splitting.tcpIp.splitModeFixed", { pos: config.fragmentation.sni_position });
  };

  return (
    <>
      <B4FormHeader label={t("sets.tcp.splitting.tcpIp.whereToSplit")} />

      <Grid size={{ xs: 12 }}>
        <B4Switch
          label={t("sets.tcp.splitting.tcpIp.smartSniSplit")}
          checked={config.fragmentation.middle_sni}
          onChange={(checked: boolean) =>
            onChange("fragmentation.middle_sni", checked)
          }
          description={t("sets.tcp.splitting.tcpIp.smartSniSplitDesc")}
          aiTopic="fragmentation.middle_sni"
        />
      </Grid>

      {/* Visual explanation */}
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
            {t("sets.tcp.splitting.tcpIp.packetStructViz")}
          </Typography>
          <Box
            sx={{
              display: "flex",
              gap: 0.5,
              fontFamily: "monospace",
              fontSize: "0.75rem",
            }}
          >
            <Box
              sx={{
                p: 1,
                bgcolor: colors.accent.primary,
                borderRadius: 0.5,
                textAlign: "center",
                minWidth: 60,
              }}
            >
              TLS Header
            </Box>
            <Box
              sx={{
                p: 1,
                bgcolor: colors.accent.secondary,
                borderRadius: 0.5,
                textAlign: "center",
                flex: 1,
                position: "relative",
              }}
            >
              {/* Fixed position split line */}
              {config.fragmentation.sni_position > 0 && (
                <Box
                  component="span"
                  sx={{
                    position: "absolute",
                    left: "20%",
                    top: 0,
                    bottom: 0,
                    width: 2,
                    bgcolor: colors.tertiary,
                    transform: "translateX(-50%)",
                  }}
                />
              )}
              {/* Middle SNI split line */}
              {config.fragmentation.middle_sni && (
                <Box
                  component="span"
                  sx={{
                    position: "absolute",
                    left: "50%",
                    top: 0,
                    bottom: 0,
                    width: 2,
                    bgcolor: colors.quaternary,
                    transform: "translateX(-50%)",
                  }}
                />
              )}
              SNI: youtube.com
            </Box>
            <Box
              sx={{
                p: 1,
                bgcolor: colors.accent.primary,
                borderRadius: 0.5,
                textAlign: "center",
                minWidth: 80,
              }}
            >
              Extensions...
            </Box>
          </Box>
          <Typography
            variant="caption"
            sx={{
              color: "text.secondary",
              mt: 1,
              display: "block"
            }}>
            {getSplitModeDescription()}
          </Typography>
        </Box>
      </Grid>

      <Grid size={{ xs: 12 }}>
        <Typography
          variant="caption"
          gutterBottom
          component="div"
          sx={{
            color: "warning.main"
          }}
        >
          {t("sets.tcp.splitting.tcpIp.manualOverride")}
        </Typography>
        <Grid container spacing={2} sx={{ mt: 1 }}>
          <Grid size={{ xs: 12, md: 12 }}>
            <B4RangeSlider
              label={t("sets.tcp.splitting.tcpIp.fixedSplitPos")}
              value={[
                config.fragmentation.sni_position,
                config.fragmentation.sni_position_max || config.fragmentation.sni_position,
              ]}
              onChange={(value: [number, number]) => {
                onChange("fragmentation.sni_position", value[0]);
                onChange("fragmentation.sni_position_max", value[1]);
              }}
              min={0}
              max={50}
              step={1}
              helperText={t("sets.tcp.splitting.tcpIp.fixedSplitPosHelper")}
              aiTopic="fragmentation.sni_position"
              aiContext={{
                middle_sni: config.fragmentation.middle_sni,
                strategy: config.fragmentation.strategy,
              }}
            />
          </Grid>
        </Grid>
        {config.fragmentation.sni_position > 0 &&
          config.fragmentation.middle_sni && (
            <B4Alert severity="info" sx={{ mt: 2 }}>
              {t("sets.tcp.splitting.tcpIp.bothEnabled")}
            </B4Alert>
          )}
      </Grid>
    </>
  );
};
