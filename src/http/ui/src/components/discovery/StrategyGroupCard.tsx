import {
  Box,
  Button,
  Stack,
  Typography,
  IconButton,
  Tooltip,
  CircularProgress,
  Collapse,
  Divider,
  Paper,
} from "@mui/material";
import { AddIcon, ExpandIcon, CollapseIcon, ImprovementIcon } from "@b4.icons";
import { colors } from "@design";
import { B4Badge } from "@b4.elements";
import {
  StrategyFamily,
  DiscoveryResult,
  DomainPresetResult,
} from "@models/discovery";
import { StrategyGroup } from "../../utils/discovery";
import { useTranslation } from "react-i18next";

interface StrategyGroupCardProps {
  group: StrategyGroup;
  expanded: boolean;
  onToggleExpand: () => void;
  onApply: () => void;
  onAddStrategy: (domain: string, result: DomainPresetResult) => void;
  addingPreset: boolean;
  familyNames: Record<StrategyFamily, string>;
  domainResults?: Record<string, DiscoveryResult>;
}

export const StrategyGroupCard = ({
  group,
  expanded,
  onToggleExpand,
  onApply,
  onAddStrategy,
  addingPreset,
  familyNames,
  domainResults,
}: StrategyGroupCardProps) => {
  const { t } = useTranslation();

  return (
    <Paper
      elevation={0}
      sx={{
        bgcolor: colors.background.paper,
        border: `1px solid ${colors.border.default}`,
        borderRadius: 2,
        overflow: "hidden",
      }}
    >
      <Box
        sx={{
          p: 2,
          bgcolor: colors.accent.primary,
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          cursor: "pointer",
        }}
        onClick={onToggleExpand}
      >
        <Box sx={{ display: "flex", alignItems: "center", gap: 2 }}>
          <IconButton size="small">
            {expanded ? <CollapseIcon /> : <ExpandIcon />}
          </IconButton>
          <Typography variant="h6" sx={{ color: colors.text.primary }}>
            {familyNames[group.family]}
          </Typography>
          {group.winnerPreset && (
            <B4Badge
              label={group.winnerPreset}
              size="small"
              variant="filled"
              color="primary"
            />
          )}
          <B4Badge
            label={t("discovery.badges.success")}
            size="small"
            variant="filled"
            color="primary"
          />
          <B4Badge
            label={t("discovery.grouped.domainCount", {
              count: group.domains.length,
            })}
            size="small"
            variant="outlined"
            color="primary"
          />
        </Box>
      </Box>

      <Box sx={{ p: 2, bgcolor: colors.background.default }}>
        <Stack
          direction="row"
          spacing={1}
          flexWrap="wrap"
          gap={1}
          sx={{ mb: 2 }}
        >
          {group.domains.map((d) => (
            <B4Badge
              key={d.domain}
              label={d.domain}
              size="small"
              color="primary"
            />
          ))}
        </Stack>
        <Box sx={{ display: "flex", justifyContent: "flex-end" }}>
          <Button
            variant="contained"
            startIcon={
              addingPreset ? (
                <CircularProgress size={18} color="inherit" />
              ) : (
                <AddIcon />
              )
            }
            onClick={onApply}
            disabled={addingPreset || !group.representativeSet}
            sx={{
              bgcolor: colors.secondary,
              color: colors.background.default,
              "&:hover": { bgcolor: colors.primary },
            }}
          >
            {group.domains.length > 1
              ? t("discovery.grouped.applyAll", {
                  count: group.domains.length,
                })
              : t("discovery.useThisStrategy")}
          </Button>
        </Box>
      </Box>

      <Collapse in={expanded}>
        <Divider sx={{ borderColor: colors.border.default }} />
        <Box sx={{ p: 2 }}>
          <Typography
            variant="subtitle2"
            sx={{
              color: colors.text.secondary,
              mb: 1.5,
              textTransform: "uppercase",
              fontSize: "0.7rem",
            }}
          >
            {t("discovery.grouped.perDomainDetails")}
          </Typography>
          <Stack spacing={1}>
            {[...group.domains]
              .sort((a, b) => b.speed - a.speed)
              .map((d) => {
                const dr = domainResults?.[d.domain];
                const successResults = dr
                  ? Object.values(dr.results)
                      .filter((r) => r.status === "complete")
                      .sort((a, b) => b.speed - a.speed)
                      .slice(0, 5)
                  : [];
                return (
                  <Box
                    key={d.domain}
                    sx={{
                      p: 1.5,
                      bgcolor: colors.background.dark,
                      borderRadius: 1,
                    }}
                  >
                    <Box
                      sx={{
                        display: "flex",
                        justifyContent: "space-between",
                        alignItems: "center",
                        mb: successResults.length > 1 ? 1 : 0,
                      }}
                    >
                      <Box
                        sx={{
                          display: "flex",
                          alignItems: "center",
                          gap: 1,
                        }}
                      >
                        <Typography
                          variant="body2"
                          sx={{
                            fontWeight: 600,
                            color: colors.text.primary,
                          }}
                        >
                          {d.domain}
                        </Typography>
                        <B4Badge
                          label={d.presetName}
                          size="small"
                          color="primary"
                        />
                        {dr?.results[dr.best_preset]?.set && (
                          <Tooltip title={t("discovery.useThisConfig")}>
                            <IconButton
                              size="small"
                              onClick={() =>
                                onAddStrategy(
                                  d.domain,
                                  dr.results[dr.best_preset],
                                )
                              }
                              disabled={addingPreset}
                              sx={{
                                p: 0.5,
                                bgcolor: colors.background.paper,
                                border: `1px solid ${colors.border.light}`,
                                "&:hover": {
                                  bgcolor: colors.accent.secondary,
                                  borderColor: colors.secondary,
                                },
                              }}
                            >
                              <AddIcon fontSize="small" />
                            </IconButton>
                          </Tooltip>
                        )}
                      </Box>
                      {!!d.improvement && d.improvement > 0 && (
                        <B4Badge
                          icon={<ImprovementIcon />}
                          label={`+${d.improvement.toFixed(0)}%`}
                          size="small"
                          color="primary"
                        />
                      )}
                    </Box>
                    {successResults.length > 1 && dr && (
                      <Stack
                        direction="row"
                        spacing={0.5}
                        flexWrap="wrap"
                        gap={0.5}
                      >
                        {successResults
                          .filter((r) => r.preset_name !== dr.best_preset)
                          .map((result, idx) => (
                            <Box
                              key={result.preset_name}
                              sx={{
                                display: "flex",
                                alignItems: "center",
                                gap: 0.5,
                              }}
                            >
                              <B4Badge
                                label={`#${idx + 2} ${result.preset_name}`}
                                size="small"
                                color="default"
                              />
                              {result.set && (
                                <Tooltip title={t("discovery.useThisConfig")}>
                                  <IconButton
                                    size="small"
                                    onClick={() =>
                                      onAddStrategy(d.domain, result)
                                    }
                                    disabled={addingPreset}
                                    sx={{
                                      p: 0.5,
                                      bgcolor: colors.background.paper,
                                      border: `1px solid ${colors.border.light}`,
                                      "&:hover": {
                                        bgcolor: colors.accent.secondary,
                                        borderColor: colors.secondary,
                                      },
                                    }}
                                  >
                                    <AddIcon fontSize="small" />
                                  </IconButton>
                                </Tooltip>
                              )}
                            </Box>
                          ))}
                      </Stack>
                    )}
                  </Box>
                );
              })}
          </Stack>
        </Box>
      </Collapse>
    </Paper>
  );
};
