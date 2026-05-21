import { Grid, Box, Typography } from "@mui/material";
import {
  B4Select,
  B4FormHeader,
  B4NumberField,
  B4TextField,
  B4PlusButton,
  B4ChipList,
  B4Hint,
} from "@b4.elements";
import { colors } from "@design";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

interface SeqOverlapPatternFieldsProps {
  pattern: string[];
  onChange: (pattern: string[]) => void;
  length: number;
  onLengthChange: (length: number) => void;
}

const normalizeByte = (b: string): string => {
  const hex = b.trim().replace(/^0x/i, "").toUpperCase().padStart(2, "0");
  return `0x${hex}`;
};

export const SeqOverlapPatternFields = ({
  pattern,
  onChange,
  length,
  onLengthChange,
}: SeqOverlapPatternFieldsProps) => {
  const { t } = useTranslation();
  const [customMode, setCustomMode] = useState(false);
  const [newByte, setNewByte] = useState("");
  const justActivatedCustom = useRef(false);
  const normalizedPattern = pattern.map(normalizeByte);
  const patternKey = normalizedPattern.join(",");

  const SEQ_OVERLAP_PRESETS = [
    {
      label: t("sets.tcp.splitting.disorder.presetNone"),
      value: "none",
      pattern: [],
    },
    {
      label: t("sets.tcp.splitting.disorder.presetTls12"),
      value: "tls12",
      pattern: ["0x16", "0x03", "0x03", "0x00", "0x00"],
    },
    {
      label: t("sets.tcp.splitting.disorder.presetTls11"),
      value: "tls11",
      pattern: ["0x16", "0x03", "0x02", "0x00", "0x00"],
    },
    {
      label: t("sets.tcp.splitting.disorder.presetTls10"),
      value: "tls10",
      pattern: ["0x16", "0x03", "0x01", "0x00", "0x00"],
    },
    {
      label: t("sets.tcp.splitting.disorder.presetHttpGet"),
      value: "http_get",
      pattern: ["0x47", "0x45", "0x54", "0x20", "0x2F"],
    },
    {
      label: t("sets.tcp.splitting.disorder.presetZeros"),
      value: "zeros",
      pattern: ["0x00"],
    },
    {
      label: t("sets.tcp.splitting.disorder.presetCustom"),
      value: "custom",
      pattern: [],
    },
  ];

  const getCurrentPreset = () => {
    if (customMode) return "custom";
    if (normalizedPattern.length === 0) return "none";

    const match = SEQ_OVERLAP_PRESETS.find(
      (p) =>
        p.value !== "none" &&
        p.value !== "custom" &&
        p.pattern.length === normalizedPattern.length &&
        p.pattern.every((b, i) => b === normalizedPattern[i]),
    );
    return match?.value || "custom";
  };

  useEffect(() => {
    if (justActivatedCustom.current) {
      justActivatedCustom.current = false;
      return;
    }
    const matchesKnownPreset = SEQ_OVERLAP_PRESETS.some(
      (p) =>
        p.value !== "custom" &&
        p.pattern.length === normalizedPattern.length &&
        p.pattern.every((b, i) => b === normalizedPattern[i]),
    );
    if (matchesKnownPreset) {
      setCustomMode(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [patternKey]);

  const handlePresetChange = (preset: string) => {
    if (preset === "none") {
      setCustomMode(false);
      onChange([]);
      return;
    }

    if (preset === "custom") {
      justActivatedCustom.current = true;
      onChange([]);
      setCustomMode(true);
      return;
    }

    setCustomMode(false);
    const found = SEQ_OVERLAP_PRESETS.find((p) => p.value === preset);
    if (found) {
      onChange(found.pattern);
    }
  };

  const handleAddByte = () => {
    const bytes: string[] = [];
    newByte.split(" ").forEach((b) => {
      const hex = b.trim().replace(/^0x/i, "").toUpperCase();
      if (/^[0-9A-F]{1,2}$/.test(hex)) {
        bytes.push(`0x${hex.padStart(2, "0")}`);
      }
    });
    onChange([...normalizedPattern, ...bytes]);
    setNewByte("");
  };

  const handleRemoveByte = (index: number) => {
    onChange(normalizedPattern.filter((_, i) => i !== index));
  };

  return (
    <>
      <B4FormHeader label={t("sets.tcp.splitting.disorder.seqOverlapHeader")} />

      <Grid size={{ xs: 12, md: 6 }}>
        <B4Select
          label={t("sets.tcp.splitting.disorder.overlapPattern")}
          value={getCurrentPreset()}
          options={SEQ_OVERLAP_PRESETS.map((p) => ({
            label: p.label,
            value: p.value,
          }))}
          onChange={(e) => handlePresetChange(e.target.value as string)}
          helperText={t("sets.tcp.splitting.disorder.overlapPatternHelper")}
          aiTopic="fragmentation.seq_overlap_pattern"
          aiContext={{
            available: SEQ_OVERLAP_PRESETS.map((p) => p.value),
            current_pattern: normalizedPattern,
          }}
        />
      </Grid>
      <Grid size={{ xs: 12, md: 6 }}>
        <B4Hint>{t("sets.tcp.splitting.disorder.seqOverlapAlert")}</B4Hint>
      </Grid>
      <Grid size={{ xs: 12, md: 6 }}>
        <B4NumberField
          label={t("sets.tcp.splitting.disorder.seqOverlapLength")}
          value={length}
          onChange={(n) => onLengthChange(n)}
          min={0}
          placeholder="0"
          helperText={t("sets.tcp.splitting.disorder.seqOverlapLengthHelper")}
          aiTopic="fragmentation.seq_overlap_length"
          aiContext={{
            current_length: length,
            pattern_set: pattern.length > 0,
          }}
        />
      </Grid>
      {normalizedPattern.length > 0 && (
        <Grid size={{ xs: 6 }}>
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
              {t("sets.tcp.splitting.disorder.seqovlViz")}
            </Typography>
            <Box
              sx={{
                display: "flex",
                gap: 0.5,
                fontFamily: "monospace",
                fontSize: "0.75rem",
                alignItems: "center",
              }}
            >
              <Box
                sx={{
                  p: 1,
                  bgcolor: colors.tertiary,
                  borderRadius: 0.5,
                  border: `2px dashed ${colors.secondary}`,
                }}
              >
                {(() => {
                  const effectiveLen =
                    length > 0 ? length : normalizedPattern.length;
                  const shown = Array.from(
                    { length: Math.min(effectiveLen, 16) },
                    (_, i) => normalizedPattern[i % normalizedPattern.length],
                  );
                  const ellipsis = effectiveLen > shown.length ? " ..." : "";
                  return `[${shown.join(" ")}${ellipsis}] (${effectiveLen}B, seq-${effectiveLen})`;
                })()}
              </Box>
              <Typography sx={{ mx: 1 }}>+</Typography>
              <Box
                sx={{
                  p: 1,
                  bgcolor: colors.accent.secondary,
                  borderRadius: 0.5,
                  flex: 1,
                }}
              >
                {t("sets.tcp.splitting.disorder.seqovlReal")}
              </Box>
            </Box>
            <Typography
              variant="caption"
              color="text.secondary"
              sx={{ mt: 1, display: "block" }}
            >
              {t("sets.tcp.splitting.disorder.seqovlNote")}
            </Typography>
          </Box>
        </Grid>
      )}
      {getCurrentPreset() === "custom" && (
        <>
          <Grid size={{ xs: 12, md: 6 }}>
            <Box sx={{ display: "flex", gap: 1 }}>
              <B4TextField
                label={t("sets.tcp.splitting.disorder.addByteLabel")}
                value={newByte}
                onChange={(e) => setNewByte(e.target.value)}
                onKeyDown={(e) => e.key === "Enter" && e.preventDefault()}
                placeholder={t(
                  "sets.tcp.splitting.disorder.addBytePlaceholder",
                )}
                size="small"
              />
              <B4PlusButton
                onClick={handleAddByte}
                disabled={!newByte.trim()}
              />
            </Box>
          </Grid>

          <B4ChipList
            items={normalizedPattern.map((b, i) => ({ byte: b, index: i }))}
            getKey={(item) => `${item.byte}-${item.index}`}
            getLabel={(item) => item.byte}
            onDelete={(item) => handleRemoveByte(item.index)}
            emptyMessage={t("sets.tcp.splitting.disorder.addByteEmpty")}
            gridSize={{ xs: 12, md: 6 }}
            showEmpty
          />
        </>
      )}
    </>
  );
};
