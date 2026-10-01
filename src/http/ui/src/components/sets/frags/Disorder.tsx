import { Grid, Box, Typography } from "@mui/material";
import { B4SetConfig, DisorderShuffleMode } from "@models/config";
import {
  B4Alert,
  B4Slider,
  B4RangeSlider,
  B4Switch,
  B4Select,
  B4FormHeader,
  B4Hint,
} from "@b4.elements";
import { colors } from "@design";
import { useTranslation } from "react-i18next";
import { SeqOverlapPatternFields } from "./SeqOverlapPatternFields";

interface DisorderSettingsProps {
  config: B4SetConfig;
  onChange: (
    field: string,
    value: string | boolean | number | string[],
  ) => void;
}

export const DisorderSettings = ({
  config,
  onChange,
}: DisorderSettingsProps) => {
  const { t } = useTranslation();
  const disorder = config.fragmentation.disorder;
  const middleSni = config.fragmentation.middle_sni;
  const seqPattern = config.fragmentation.seq_overlap_pattern || [];

  const shuffleModeOptions: { label: string; value: DisorderShuffleMode }[] = [
    { label: t("sets.tcp.splitting.disorder.shuffleFull"), value: "full" },
    {
      label: t("sets.tcp.splitting.disorder.shuffleReverse"),
      value: "reverse",
    },
  ];

  return (
    <>
      <B4FormHeader label={t("sets.tcp.splitting.disorder.header")} />
      <B4Hint>{t("sets.tcp.splitting.disorder.alert")}</B4Hint>

      {/* SNI Split Toggle */}
      <Grid size={{ xs: 12, md: 6 }}>
        <B4Switch
          label={t("sets.tcp.splitting.disorder.sniSplit")}
          checked={middleSni}
          onChange={(checked: boolean) =>
            onChange("fragmentation.middle_sni", checked)
          }
          description={t("sets.tcp.splitting.disorder.sniSplitDesc")}
          aiTopic="fragmentation.middle_sni"
        />
      </Grid>

      <Grid size={{ xs: 12, md: 6 }}>
        <B4Select
          label={t("sets.tcp.splitting.disorder.shuffleMode")}
          value={disorder.shuffle_mode}
          options={shuffleModeOptions}
          onChange={(e) =>
            onChange("fragmentation.disorder.shuffle_mode", e.target.value)
          }
          helperText={t("sets.tcp.splitting.disorder.shuffleHelper")}
          aiTopic="fragmentation.disorder.shuffle_mode"
          aiContext={{ available: shuffleModeOptions.map((o) => o.value) }}
        />
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
            {t("sets.tcp.splitting.disorder.segOrderExample")}
          </Typography>
          <Box sx={{ display: "flex", gap: 1, alignItems: "center" }}>
            <Box sx={{ display: "flex", gap: 0.5, fontFamily: "monospace" }}>
              {["①", "②", "③", "④"].map((n) => (
                <Box
                  key={n}
                  sx={{
                    p: 1,
                    bgcolor: colors.accent.primary,
                    borderRadius: 0.5,
                    minWidth: 32,
                    textAlign: "center",
                  }}
                >
                  {n}
                </Box>
              ))}
            </Box>
            <Typography sx={{ mx: 2 }}>→</Typography>
            <Box sx={{ display: "flex", gap: 0.5, fontFamily: "monospace" }}>
              {(disorder.shuffle_mode === "reverse"
                ? ["④", "③", "②", "①"]
                : ["③", "①", "④", "②"]
              ).map((n) => (
                <Box
                  key={n}
                  sx={{
                    p: 1,
                    bgcolor: colors.tertiary,
                    borderRadius: 0.5,
                    minWidth: 32,
                    textAlign: "center",
                  }}
                >
                  {n}
                </Box>
              ))}
            </Box>
          </Box>
          <Typography
            variant="caption"
            sx={{
              color: "text.secondary",
              mt: 1,
              display: "block"
            }}>
            {disorder.shuffle_mode === "full"
              ? t("sets.tcp.splitting.disorder.segRandomOrder")
              : t("sets.tcp.splitting.disorder.segReverseOrder")}
          </Typography>
        </Box>
      </Grid>

      <B4FormHeader label={t("sets.tcp.splitting.disorder.timingHeader")} />
      <B4Hint>{t("sets.tcp.splitting.disorder.timingAlert")}</B4Hint>

      <Grid size={{ xs: 12, md: 6 }}>
        <B4Slider
          label={t("sets.tcp.splitting.disorder.minJitter")}
          value={disorder.min_jitter_us}
          onChange={(value: number) =>
            onChange("fragmentation.disorder.min_jitter_us", value)
          }
          min={100}
          max={5000}
          step={100}
          helperText={t("sets.tcp.splitting.disorder.minJitterHelper")}
          aiTopic="fragmentation.disorder.jitter"
          aiContext={{
            min_jitter_us: disorder.min_jitter_us,
            max_jitter_us: disorder.max_jitter_us,
          }}
        />
      </Grid>

      <Grid size={{ xs: 12, md: 6 }}>
        <B4Slider
          label={t("sets.tcp.splitting.disorder.maxJitter")}
          value={disorder.max_jitter_us}
          onChange={(value: number) =>
            onChange("fragmentation.disorder.max_jitter_us", value)
          }
          min={500}
          max={10000}
          step={100}
          helperText={t("sets.tcp.splitting.disorder.maxJitterHelper")}
          aiTopic="fragmentation.disorder.jitter"
          aiContext={{
            min_jitter_us: disorder.min_jitter_us,
            max_jitter_us: disorder.max_jitter_us,
          }}
        />
      </Grid>

      {disorder.min_jitter_us >= disorder.max_jitter_us && (
        <B4Alert severity="warning">
          {t("sets.tcp.splitting.disorder.jitterWarning")}
        </B4Alert>
      )}

      <B4FormHeader label={t("sets.tcp.splitting.disorder.fakePerSegHeader")} />

      <Grid size={{ xs: 12 }}>
        <B4Hint>{t("sets.tcp.splitting.disorder.fakePerSegAlert")}</B4Hint>
      </Grid>

      <Grid size={{ xs: 12, md: 6 }}>
        <B4Switch
          label={t("sets.tcp.splitting.disorder.fakePerSeg")}
          checked={disorder.fake_per_segment}
          onChange={(checked: boolean) =>
            onChange("fragmentation.disorder.fake_per_segment", checked)
          }
          description={t("sets.tcp.splitting.disorder.fakePerSegDesc")}
          aiTopic="fragmentation.disorder.fake_per_segment"
          aiContext={{
            fake_per_seg_count: disorder.fake_per_seg_count,
            fake_per_seg_count_max: disorder.fake_per_seg_count_max,
            seq_overlap_pattern_set:
              (config.fragmentation.seq_overlap_pattern || []).length > 0,
          }}
        />
      </Grid>

      {disorder.fake_per_segment && (
        <Grid size={{ xs: 12, md: 6 }}>
          <B4RangeSlider
            label={t("sets.tcp.splitting.disorder.fakesPerSeg")}
            value={[
              disorder.fake_per_seg_count || 1,
              disorder.fake_per_seg_count_max ||
                disorder.fake_per_seg_count ||
                1,
            ]}
            onChange={(value: [number, number]) => {
              onChange("fragmentation.disorder.fake_per_seg_count", value[0]);
              onChange(
                "fragmentation.disorder.fake_per_seg_count_max",
                value[1],
              );
            }}
            min={1}
            max={11}
            step={1}
            helperText={t("sets.tcp.splitting.disorder.fakesPerSegHelper")}
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
