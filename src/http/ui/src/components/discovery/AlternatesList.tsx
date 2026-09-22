import { useState } from "react";
import { Box, Button, Stack, Tooltip, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";
import { colors, typography } from "@design";
import { B4Badge } from "@b4.elements";
import { AppliedMark, StrategyFamily } from "@models/discovery";
import { Alternate, formatSpeed } from "@utils";

interface AlternatesListProps {
  alternates: Alternate[];
  applied?: Record<string, AppliedMark>;
  busy?: boolean;
  preview?: number;
  onUse: (alt: Alternate) => void;
}

const PREVIEW = 8;

export const AlternatesList = ({
  alternates,
  applied,
  busy,
  preview = PREVIEW,
  onUse,
}: AlternatesListProps) => {
  const { t } = useTranslation();
  const [showAll, setShowAll] = useState(false);

  if (alternates.length === 0) return null;

  const shown = showAll ? alternates : alternates.slice(0, preview);
  const familyName = (family?: StrategyFamily) =>
    family
      ? t(`discovery.familyNames.${family}`, { defaultValue: family })
      : "";

  return (
    <Box>
      <Typography
        variant="caption"
        sx={{ color: colors.text.secondary, display: "block", mb: 0.5 }}
      >
        {t("discovery.results.alsoWorked", { count: alternates.length })}
      </Typography>
      <Stack spacing={0.5}>
        {shown.map((alt) => {
          const mark = applied?.[alt.preset];
          return (
            <Box
              key={alt.preset}
              sx={{
                display: "grid",
                gridTemplateColumns: "auto 1fr auto",
                gap: 1.5,
                alignItems: "center",
              }}
            >
              <B4Badge
                variant="outlined"
                label={alt.preset}
                sx={{
                  fontFamily: typography.recipes.monoSmall.fontFamily,
                  fontSize: typography.sizes.sm,
                }}
              />
              <Typography
                variant="caption"
                noWrap
                sx={{ color: colors.text.secondary }}
              >
                {familyName(alt.family)}
                {alt.speed > 0 ? ` · ${formatSpeed(alt.speed)}` : ""}
              </Typography>
              <Box
                sx={{ display: "inline-flex", alignItems: "center", gap: 0.5 }}
              >
                {mark && (
                  <Tooltip
                    title={t("discovery.results.triedOn", {
                      when: new Date(mark.at).toLocaleString(),
                    })}
                  >
                    <span>
                      <B4Badge
                        variant="outlined"
                        color="secondary"
                        label={t("discovery.results.tried")}
                      />
                    </span>
                  </Tooltip>
                )}
                <Button
                  size="small"
                  disabled={busy}
                  onClick={() => onUse(alt)}
                  sx={{ textTransform: "none", minWidth: 0 }}
                >
                  {t("discovery.results.useInstead")}
                </Button>
              </Box>
            </Box>
          );
        })}
      </Stack>
      {alternates.length > preview && (
        <Button
          size="small"
          onClick={() => setShowAll((v) => !v)}
          sx={{
            textTransform: "none",
            mt: 0.5,
            color: colors.text.secondary,
          }}
        >
          {showAll
            ? t("discovery.results.showFewer")
            : t("discovery.results.showAll", { count: alternates.length })}
        </Button>
      )}
    </Box>
  );
};
