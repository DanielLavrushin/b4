import { Grid, Box, Typography, Stack } from "@mui/material";
import { B4RangeSlider, B4Switch, B4Select } from "@b4.fields";
import { B4SetConfig, ComboShuffleMode } from "@models/config";
import { colors } from "@design";
import { B4Alert, B4FormHeader, B4Hint } from "@b4.elements";
import { useTranslation } from "react-i18next";
import { SeqOverlapPatternFields } from "./SeqOverlapPatternFields";

interface ComboSettingsProps {
  config: B4SetConfig;
  onChange: (
    field: string,
    value: string | boolean | number | string[],
  ) => void;
}

export const ComboSettings = ({ config, onChange }: ComboSettingsProps) => {
  const { t } = useTranslation();
  const combo = config.fragmentation.combo;
  const middleSni = config.fragmentation.middle_sni;
  const seqPattern = config.fragmentation.seq_overlap_pattern || [];

  const shuffleModeOptions: { label: string; value: ComboShuffleMode }[] = [
    { label: t("sets.tcp.splitting.combo.shuffleMiddle"), value: "middle" },
    { label: t("sets.tcp.splitting.combo.shuffleFull"), value: "full" },
    { label: t("sets.tcp.splitting.combo.shuffleReverse"), value: "reverse" },
  ];

  const enabledSplits = [
    combo.first_byte_split && "1st byte",
    combo.extension_split && "ext",
    middleSni && "SNI",
  ].filter(Boolean);

  return (
    <>
      <B4FormHeader label={t("sets.tcp.splitting.combo.header")} />

      <B4Hint>{t("sets.tcp.splitting.combo.alert")}</B4Hint>

      <Grid size={{ xs: 12 }}>
        <Stack spacing={2} direction={"row"} sx={{
          mt: 1
        }}>
          <B4Switch
            label={t("sets.tcp.splitting.combo.firstByte")}
            checked={combo.first_byte_split}
            onChange={(checked: boolean) =>
              onChange("fragmentation.combo.first_byte_split", checked)
            }
            description={t("sets.tcp.splitting.combo.firstByteDesc")}
            aiTopic="fragmentation.combo.first_byte_split"
          />

          <B4Switch
            label={t("sets.tcp.splitting.combo.extensionSplit")}
            checked={combo.extension_split}
            onChange={(checked: boolean) =>
              onChange("fragmentation.combo.extension_split", checked)
            }
            description={t("sets.tcp.splitting.combo.extensionSplitDesc")}
            aiTopic="fragmentation.combo.extension_split"
          />

          <B4Switch
            label={t("sets.tcp.splitting.combo.sniSplit")}
            checked={middleSni}
            onChange={(checked: boolean) =>
              onChange("fragmentation.middle_sni", checked)
            }
            description={t("sets.tcp.splitting.combo.sniSplitDesc")}
            aiTopic="fragmentation.middle_sni"
          />
        </Stack>
      </Grid>

      {/* Visual */}
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
            {t("sets.tcp.splitting.combo.segmentViz")}
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
            {combo.first_byte_split && (
              <Box
                sx={{
                  p: 1,
                  bgcolor: colors.tertiary,
                  borderRadius: 0.5,
                  textAlign: "center",
                  minWidth: 40,
                }}
              >
                1B
              </Box>
            )}
            {combo.extension_split && (
              <Box
                sx={{
                  p: 1,
                  bgcolor: colors.accent.primary,
                  borderRadius: 0.5,
                  textAlign: "center",
                  flex: 1,
                  minWidth: 60,
                }}
              >
                Pre-SNI Ext
              </Box>
            )}
            {middleSni && (
              <>
                <Box
                  sx={{
                    p: 1,
                    bgcolor: colors.accent.secondary,
                    borderRadius: 0.5,
                    textAlign: "center",
                    minWidth: 50,
                  }}
                >
                  SNI₁
                </Box>
                <Box
                  sx={{
                    p: 1,
                    bgcolor: colors.accent.secondary,
                    borderRadius: 0.5,
                    textAlign: "center",
                    minWidth: 50,
                  }}
                >
                  SNI₂
                </Box>
              </>
            )}
            <Box
              sx={{
                p: 1,
                bgcolor: colors.quaternary,
                borderRadius: 0.5,
                textAlign: "center",
                flex: 1,
                minWidth: 60,
              }}
            >
              Rest...
            </Box>
          </Box>
          <Typography
            variant="caption"
            sx={{
              color: "text.secondary",
              mt: 1,
              display: "block"
            }}>
            {enabledSplits.length > 0
              ? t("sets.tcp.splitting.combo.activeSplits", {
                  splits: enabledSplits.join(" → "),
                  count: enabledSplits.length + 1,
                })
              : t("sets.tcp.splitting.combo.noSplits")}
          </Typography>
        </Box>
      </Grid>

      <Grid size={{ xs: 12 }}>
        <B4Switch
          label={t("sets.tcp.splitting.combo.decoyEnable")}
          checked={combo.decoy_enabled}
          onChange={(checked: boolean) =>
            onChange("fragmentation.combo.decoy_enabled", checked)
          }
          description={t("sets.tcp.splitting.combo.decoyDesc")}
          aiTopic="fragmentation.combo.decoy_enabled"
        />
      </Grid>
      {combo.decoy_enabled && (
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
              {t("sets.tcp.splitting.combo.decoyHow")}
            </Typography>
            <Box sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
              <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
                <Typography
                  variant="caption"
                  sx={{ minWidth: 80, color: colors.text.secondary }}
                >
                  {t("sets.tcp.splitting.combo.decoySent1st")}
                </Typography>
                <Box
                  sx={{
                    p: 1,
                    bgcolor: colors.tertiary,
                    borderRadius: 0.5,
                    fontFamily: "monospace",
                    fontSize: "0.7rem",
                    border: `2px dashed ${colors.secondary}`,
                  }}
                >
                  {t("sets.tcp.splitting.combo.decoyFakePayload")}
                </Box>
                <Typography
                  variant="caption"
                  sx={{ color: colors.secondary, ml: 1 }}
                >
                  {t("sets.tcp.splitting.combo.decoyFakeNote")}
                </Typography>
              </Box>
              <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
                <Typography
                  variant="caption"
                  sx={{ minWidth: 80, color: colors.text.secondary }}
                >
                  {t("sets.tcp.splitting.combo.decoySent2nd")}
                </Typography>
                <Box
                  sx={{
                    display: "flex",
                    gap: 0.5,
                    fontFamily: "monospace",
                    fontSize: "0.7rem",
                  }}
                >
                  <Box
                    sx={{
                      p: 1,
                      bgcolor: colors.accent.secondary,
                      borderRadius: 0.5,
                      border: `2px solid ${colors.secondary}`,
                    }}
                  >
                    {t("sets.tcp.splitting.combo.decoyRealPayload")}
                  </Box>
                </Box>
                <Typography
                  variant="caption"
                  sx={{ color: colors.secondary, ml: 1 }}
                >
                  {t("sets.tcp.splitting.combo.decoyRealNote")}
                </Typography>
              </Box>
            </Box>
          </Box>
        </Grid>
      )}

      {/* Shuffle Mode */}
      <Grid size={{ xs: 12, md: 6 }}>
        <B4Select
          label={t("sets.tcp.splitting.combo.shuffleMode")}
          value={combo.shuffle_mode}
          options={shuffleModeOptions}
          onChange={(e) =>
            onChange("fragmentation.combo.shuffle_mode", e.target.value)
          }
          helperText={t("sets.tcp.splitting.combo.shuffleHelper")}
          aiTopic="fragmentation.combo.shuffle_mode"
          aiContext={{ available: shuffleModeOptions.map((o) => o.value) }}
        />
      </Grid>

      <Grid size={{ xs: 12, md: 6 }}>
        <B4Hint sx={{ my: 0 }}>
          {combo.shuffle_mode === "middle" &&
            t("sets.tcp.splitting.combo.shuffleMiddleDesc")}
          {combo.shuffle_mode === "full" &&
            t("sets.tcp.splitting.combo.shuffleFullDesc")}
          {combo.shuffle_mode === "reverse" &&
            t("sets.tcp.splitting.combo.shuffleReverseDesc")}
        </B4Hint>
      </Grid>

      <B4FormHeader label={t("sets.tcp.splitting.combo.timingHeader")} />

      <Grid size={{ xs: 12, md: 6 }}>
        <B4RangeSlider
          label={t("sets.tcp.splitting.combo.firstDelay")}
          value={[
            combo.first_delay_ms,
            combo.first_delay_ms_max || combo.first_delay_ms,
          ]}
          onChange={(value: [number, number]) => {
            onChange("fragmentation.combo.first_delay_ms", value[0]);
            onChange("fragmentation.combo.first_delay_ms_max", value[1]);
          }}
          min={10}
          max={500}
          step={10}
          valueSuffix=" ms"
          helperText={t("sets.tcp.splitting.combo.firstDelayHelper")}
          aiTopic="fragmentation.combo.first_delay_ms"
        />
      </Grid>

      <Grid size={{ xs: 12, md: 6 }}>
        <B4RangeSlider
          label={t("sets.tcp.splitting.combo.jitterMax")}
          value={[
            combo.jitter_max_us,
            combo.jitter_max_us_max || combo.jitter_max_us,
          ]}
          onChange={(value: [number, number]) => {
            onChange("fragmentation.combo.jitter_max_us", value[0]);
            onChange("fragmentation.combo.jitter_max_us_max", value[1]);
          }}
          min={100}
          max={10000}
          step={100}
          valueSuffix=" μs"
          helperText={t("sets.tcp.splitting.combo.jitterMaxHelper")}
          aiTopic="fragmentation.combo.jitter_max_us"
        />
      </Grid>

      <B4FormHeader label={t("sets.tcp.splitting.combo.fakePerSegHeader")} />

      <Grid size={{ xs: 12, md: 4 }}>
        <B4Switch
          label={t("sets.tcp.splitting.combo.fakePerSeg")}
          checked={combo.fake_per_segment}
          onChange={(checked: boolean) =>
            onChange("fragmentation.combo.fake_per_segment", checked)
          }
          description={t("sets.tcp.splitting.combo.fakePerSegDesc")}
          aiTopic="fragmentation.combo.fake_per_segment"
        />
      </Grid>
      <Grid size={{ xs: 12, md: 8 }}>
        <B4Hint>{t("sets.tcp.splitting.combo.fakePerSegAlert")}</B4Hint>
      </Grid>

      {combo.fake_per_segment && (
        <Grid size={{ xs: 12, md: 6 }}>
          <B4RangeSlider
            label={t("sets.tcp.splitting.combo.fakesPerSeg")}
            value={[
              combo.fake_per_seg_count || 1,
              combo.fake_per_seg_count_max || combo.fake_per_seg_count || 1,
            ]}
            onChange={(value: [number, number]) => {
              onChange("fragmentation.combo.fake_per_seg_count", value[0]);
              onChange("fragmentation.combo.fake_per_seg_count_max", value[1]);
            }}
            min={1}
            max={11}
            step={1}
            helperText={t("sets.tcp.splitting.combo.fakesPerSegHelper")}
          />
        </Grid>
      )}

      {!combo.first_byte_split && !combo.extension_split && !middleSni && (
        <B4Alert severity="warning">
          {t("sets.tcp.splitting.combo.noSplitPointsWarning")}
        </B4Alert>
      )}

      <SeqOverlapPatternFields
        pattern={seqPattern}
        onChange={(value) =>
          onChange("fragmentation.seq_overlap_pattern", value)
        }
        length={config.fragmentation.seq_overlap_length || 0}
        onLengthChange={(value) =>
          onChange("fragmentation.seq_overlap_length", value)
        }
      />
    </>
  );
};
