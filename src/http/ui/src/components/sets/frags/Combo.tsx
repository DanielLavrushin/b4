import { useMemo } from "react";
import { Grid, Box, Typography } from "@mui/material";
import { B4RangeSlider, B4Switch, B4Select } from "@b4.fields";
import { B4SetConfig, ComboShuffleMode } from "@models/config";
import { createDefaultSet } from "@models/defaults";
import { colors } from "@design";
import { B4Advanced, B4FieldMessage, B4FormHeader } from "@b4.elements";
import { useTranslation } from "react-i18next";
import { SeqOverlapPatternFields } from "./SeqOverlapPatternFields";

const TIMING_PATHS = [
  "fragmentation.combo.first_delay_ms",
  "fragmentation.combo.first_delay_ms_max",
  "fragmentation.combo.jitter_max_us",
  "fragmentation.combo.jitter_max_us_max",
];

const shownRange = (low: number, high: number): string =>
  `${low}-${high || low}`;

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
  const defaults = useMemo(() => createDefaultSet(0).fragmentation.combo, []);
  const timingCustomized = [
    shownRange(combo.first_delay_ms, combo.first_delay_ms_max) !==
      shownRange(defaults.first_delay_ms, defaults.first_delay_ms_max),
    shownRange(combo.jitter_max_us, combo.jitter_max_us_max) !==
      shownRange(defaults.jitter_max_us, defaults.jitter_max_us_max),
  ].filter(Boolean).length;

  const shuffleModeOptions: {
    label: string;
    value: ComboShuffleMode;
    note: string;
  }[] = [
    {
      label: t("sets.tcp.splitting.combo.shuffleMiddle"),
      value: "middle",
      note: t("sets.tcp.splitting.combo.shuffleMiddleDesc"),
    },
    {
      label: t("sets.tcp.splitting.combo.shuffleFull"),
      value: "full",
      note: t("sets.tcp.splitting.combo.shuffleFullDesc"),
    },
    {
      label: t("sets.tcp.splitting.combo.shuffleReverse"),
      value: "reverse",
      note: t("sets.tcp.splitting.combo.shuffleReverseDesc"),
    },
  ];

  const enabledSplits = [
    combo.first_byte_split && "1st byte",
    combo.extension_split && "ext",
    middleSni && "SNI",
  ].filter(Boolean);
  const noSplitPoints =
    !combo.first_byte_split && !combo.extension_split && !middleSni;

  return (
    <>
      <B4FormHeader label={t("sets.tcp.splitting.combo.header")} />

      <Grid size={{ xs: 12, lg: 4 }}>
        <B4Switch
          label={t("sets.tcp.splitting.combo.firstByte")}
          checked={combo.first_byte_split}
          path="fragmentation.combo.first_byte_split"
          onChange={(checked: boolean) =>
            onChange("fragmentation.combo.first_byte_split", checked)
          }
          description={t("sets.tcp.splitting.combo.firstByteDesc")}
          learnMore="sets/tcp/splitting#combo"
          aiTopic="fragmentation.combo.first_byte_split"
        />
      </Grid>

      <Grid size={{ xs: 12, lg: 4 }}>
        <B4Switch
          checked={combo.extension_split}
          path="fragmentation.combo.extension_split"
          onChange={(checked: boolean) =>
            onChange("fragmentation.combo.extension_split", checked)
          }
          description={t("sets.tcp.splitting.combo.extensionSplitDesc")}
          learnMore="sets/tcp/splitting#combo"
          aiTopic="fragmentation.combo.extension_split"
        />
      </Grid>

      <Grid size={{ xs: 12, lg: 4 }}>
        <B4Switch
          label={t("sets.tcp.splitting.combo.sniSplit")}
          checked={middleSni}
          path="fragmentation.middle_sni"
          onChange={(checked: boolean) =>
            onChange("fragmentation.middle_sni", checked)
          }
          description={t("sets.tcp.splitting.combo.sniSplitDesc")}
          learnMore="sets/tcp/splitting#combo"
          aiTopic="fragmentation.middle_sni"
        />
      </Grid>

      {noSplitPoints && (
        <Grid size={{ xs: 12 }}>
          <B4FieldMessage>
            {t("sets.tcp.splitting.combo.noSplitPointsWarning")}
          </B4FieldMessage>
        </Grid>
      )}

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
            color="text.secondary"
            sx={{ mt: 1, display: "block" }}
          >
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
          path="fragmentation.combo.decoy_enabled"
          onChange={(checked: boolean) =>
            onChange("fragmentation.combo.decoy_enabled", checked)
          }
          description={t("sets.tcp.splitting.combo.decoyDesc")}
          info={t("sets.tcp.splitting.combo.decoyInfo")}
          learnMore="sets/tcp/splitting#combo"
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
              color="text.secondary"
              component="div"
              sx={{ mb: 1 }}
            >
              {t("sets.tcp.splitting.combo.decoyHow")}
            </Typography>
            <Box sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
              <Box
                sx={{
                  display: "flex",
                  alignItems: "center",
                  flexWrap: "wrap",
                  gap: 1,
                }}
              >
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
              <Box
                sx={{
                  display: "flex",
                  alignItems: "center",
                  flexWrap: "wrap",
                  gap: 1,
                }}
              >
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

      <Grid size={{ xs: 12, md: 6 }}>
        <B4Select
          value={combo.shuffle_mode}
          options={shuffleModeOptions}
          path="fragmentation.combo.shuffle_mode"
          onChange={(e) =>
            onChange("fragmentation.combo.shuffle_mode", e.target.value)
          }
          helperText={t(
            {
              middle: "sets.tcp.splitting.combo.shuffleMiddleDesc",
              full: "sets.tcp.splitting.combo.shuffleFullDesc",
              reverse: "sets.tcp.splitting.combo.shuffleReverseDesc",
            }[combo.shuffle_mode],
          )}
          info={t("sets.tcp.splitting.combo.shuffleModeInfo")}
          learnMore="sets/tcp/splitting#combo"
          aiTopic="fragmentation.combo.shuffle_mode"
          aiContext={{ available: shuffleModeOptions.map((o) => o.value) }}
        />
      </Grid>

      <Grid size={{ xs: 12 }}>
        <B4Advanced
          title={t("sets.tcp.splitting.combo.timingHeader")}
          paths={TIMING_PATHS}
          customized={timingCustomized}
        >
          <Grid container spacing={3}>
            <Grid size={{ xs: 12, md: 6 }}>
              <B4RangeSlider
                label={t("sets.tcp.splitting.combo.firstDelay")}
                value={[
                  combo.first_delay_ms,
                  combo.first_delay_ms_max || combo.first_delay_ms,
                ]}
                path="fragmentation.combo.first_delay_ms"
                onChange={(value: [number, number]) => {
                  onChange("fragmentation.combo.first_delay_ms", value[0]);
                  onChange("fragmentation.combo.first_delay_ms_max", value[1]);
                }}
                min={10}
                max={500}
                step={10}
                valueSuffix={t("core.units.ms")}
                helperText={t("sets.tcp.splitting.combo.firstDelayHelper")}
                learnMore="sets/tcp/splitting#combo"
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
                path="fragmentation.combo.jitter_max_us"
                onChange={(value: [number, number]) => {
                  onChange("fragmentation.combo.jitter_max_us", value[0]);
                  onChange("fragmentation.combo.jitter_max_us_max", value[1]);
                }}
                min={100}
                max={10000}
                step={100}
                valueSuffix={t("core.units.us")}
                helperText={t("sets.tcp.splitting.combo.jitterMaxHelper")}
                learnMore="sets/tcp/splitting#combo"
                aiTopic="fragmentation.combo.jitter_max_us"
              />
            </Grid>
          </Grid>
        </B4Advanced>
      </Grid>

      <B4FormHeader label={t("sets.tcp.splitting.combo.fakePerSegHeader")} />

      <Grid size={{ xs: 12, md: 6 }}>
        <B4Switch
          label={t("sets.tcp.splitting.combo.fakePerSeg")}
          checked={combo.fake_per_segment}
          path="fragmentation.combo.fake_per_segment"
          onChange={(checked: boolean) =>
            onChange("fragmentation.combo.fake_per_segment", checked)
          }
          description={t("sets.tcp.splitting.combo.fakePerSegDesc")}
          message={
            combo.fake_per_segment && seqPattern.length === 0 ? (
              <B4FieldMessage>
                {t("sets.tcp.splitting.combo.fakePerSegNeedsPattern")}
              </B4FieldMessage>
            ) : undefined
          }
          info={t("sets.tcp.splitting.combo.fakePerSegInfo")}
          learnMore="sets/tcp/splitting#combo"
          aiTopic="fragmentation.combo.fake_per_segment"
        />
      </Grid>

      {combo.fake_per_segment && (
        <Grid size={{ xs: 12, md: 6 }}>
          <B4RangeSlider
            label={t("sets.tcp.splitting.combo.fakesPerSeg")}
            value={[
              combo.fake_per_seg_count || 1,
              combo.fake_per_seg_count_max || combo.fake_per_seg_count || 1,
            ]}
            path="fragmentation.combo.fake_per_seg_count"
            onChange={(value: [number, number]) => {
              onChange("fragmentation.combo.fake_per_seg_count", value[0]);
              onChange("fragmentation.combo.fake_per_seg_count_max", value[1]);
            }}
            min={1}
            max={11}
            step={1}
            helperText={t("sets.tcp.splitting.combo.fakesPerSegHelper")}
            learnMore="sets/tcp/splitting#combo"
          />
        </Grid>
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
