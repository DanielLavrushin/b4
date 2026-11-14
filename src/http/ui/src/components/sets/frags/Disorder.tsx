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
  B4NumberField,
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
  const overlapLength = config.fragmentation.seq_overlap_length || 0;
  const splitPosition = disorder.split_position || 0;
  const singleCut = !middleSni && splitPosition > 0;
  const segmentsInOrder = singleCut ? ["①", "②"] : ["①", "②", "③", "④"];
  const segmentsShuffled = singleCut ? ["②", "①"] : ["③", "①", "④", "②"];

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
          path="fragmentation.middle_sni"
          onChange={(checked: boolean) =>
            onChange("fragmentation.middle_sni", checked)
          }
          description={t("sets.tcp.splitting.disorder.sniSplitDesc")}
          aiTopic="fragmentation.middle_sni"
        />
      </Grid>

      <Grid size={{ xs: 12, md: 6 }}>
        <B4Select
          value={disorder.shuffle_mode}
          options={shuffleModeOptions}
          path="fragmentation.disorder.shuffle_mode"
          onChange={(e) =>
            onChange("fragmentation.disorder.shuffle_mode", e.target.value)
          }
          helperText={t("sets.tcp.splitting.disorder.shuffleHelper")}
          aiTopic="fragmentation.disorder.shuffle_mode"
          aiContext={{ available: shuffleModeOptions.map((o) => o.value) }}
        />
      </Grid>

      {!middleSni && (
        <Grid size={{ xs: 12, md: 6 }}>
          <B4NumberField
            label={t("sets.tcp.splitting.disorder.splitPos")}
            value={splitPosition}
            path="fragmentation.disorder.split_position"
            onChange={(value) =>
              onChange("fragmentation.disorder.split_position", value)
            }
            min={0}
            max={1460}
            placeholder="0"
            helperText={t("sets.tcp.splitting.disorder.splitPosHelper")}
            aiTopic="fragmentation.disorder.split_position"
            aiContext={{
              middle_sni: middleSni,
              seq_overlap_length: overlapLength,
            }}
          />
        </Grid>
      )}

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
            color="text.secondary"
            component="div"
            sx={{ mb: 1 }}
          >
            {t("sets.tcp.splitting.disorder.segOrderExample")}
          </Typography>
          <Box sx={{ display: "flex", gap: 1, alignItems: "center" }}>
            <Box sx={{ display: "flex", gap: 0.5, fontFamily: "monospace" }}>
              {segmentsInOrder.map((n) => (
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
                ? [...segmentsInOrder].reverse()
                : segmentsShuffled
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
            color="text.secondary"
            sx={{ mt: 1, display: "block" }}
          >
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
          path="fragmentation.disorder.min_jitter_us"
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
          path="fragmentation.disorder.max_jitter_us"
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
          path="fragmentation.disorder.fake_per_segment"
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
            path="fragmentation.disorder.fake_per_seg_count"
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
        length={overlapLength}
        onLengthChange={(value) =>
          onChange("fragmentation.seq_overlap_length", value)
        }
      />

      {overlapLength > 0 && middleSni && (
        <B4Hint>{t("sets.tcp.splitting.disorder.overlapSniHint")}</B4Hint>
      )}

      {overlapLength > 0 && !middleSni && splitPosition <= overlapLength && (
        <B4Alert severity="warning">
          {t("sets.tcp.splitting.disorder.overlapNoRoomWarning")}
        </B4Alert>
      )}
    </>
  );
};
