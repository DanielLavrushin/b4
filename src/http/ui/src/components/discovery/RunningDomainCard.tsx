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
import { AddIcon, SpeedIcon, ExpandIcon, CollapseIcon } from "@b4.icons";
import { colors } from "@design";
import { B4Badge } from "@b4.elements";
import {
  StrategyFamily,
  DiscoveryResult,
  DomainPresetResult,
} from "@models/discovery";
import { useTranslation } from "react-i18next";

interface RunningDomainCardProps {
  domainResult: DiscoveryResult;
  expanded: boolean;
  onToggleExpand: () => void;
  onAddStrategy: (domain: string, result: DomainPresetResult) => void;
  addingPreset: boolean;
  familyNames: Record<StrategyFamily, string>;
  totalSuiteChecks: number;
  running: boolean;
}

export const RunningDomainCard = ({
  domainResult,
  expanded,
  onToggleExpand,
  onAddStrategy,
  addingPreset,
  familyNames,
  totalSuiteChecks,
  running,
}: RunningDomainCardProps) => {
  const { t } = useTranslation();

  const totalCount = Object.keys(domainResult.results).length;
  const successResults = Object.values(domainResult.results)
    .filter((r) => r.status === "complete")
    .sort((a, b) => b.speed - a.speed);
  const failedCount = Object.values(domainResult.results).filter(
    (r) => r.status === "failed",
  ).length;

  const getDomainStatusBadge = () => {
    if (domainResult.baseline_works) {
      return (
        <B4Badge
          label={t("discovery.noBypass.badge")}
          size="small"
          variant="filled"
          color="success"
        />
      );
    }
    if (domainResult.best_success) {
      return (
        <B4Badge
          label={t("discovery.badges.success")}
          size="small"
          variant="filled"
          color="primary"
        />
      );
    }
    if (running) {
      return (
        <B4Badge
          label={t("discovery.badges.testing")}
          size="small"
          variant="outlined"
          color="primary"
        />
      );
    }
    if (domainResult.dns_result?.transport_blocked) {
      return (
        <B4Badge
          label={t("discovery.badges.blocked")}
          size="small"
          color="info"
        />
      );
    }
    return <B4Badge label={t("core.failed")} size="small" color="error" />;
  };

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
            {domainResult.domain}
          </Typography>
          {getDomainStatusBadge()}
          <B4Badge
            label={`${successResults.length}/${totalCount}`}
            size="small"
            variant="outlined"
            color="primary"
          />
        </Box>
        {!domainResult.best_success && (
          <Typography
            variant="h6"
            sx={{ color: colors.text.secondary, fontWeight: 600 }}
          >
            {t("discovery.tested", { count: totalCount })}
          </Typography>
        )}
      </Box>
      {domainResult.best_success && !domainResult.baseline_works && (
        <Box
          sx={{
            p: 2,
            bgcolor: colors.background.default,
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
          }}
        >
          <Box sx={{ display: "flex", alignItems: "center", gap: 2 }}>
            <SpeedIcon sx={{ color: colors.secondary }} />
            <Box>
              <Typography
                variant="caption"
                sx={{ color: colors.text.secondary }}
              >
                {t("discovery.currentBest")}
              </Typography>
              <Typography
                variant="body1"
                sx={{
                  color: colors.text.primary,
                  fontWeight: 600,
                }}
              >
                {domainResult.best_preset}
                {domainResult.results[domainResult.best_preset]?.family && (
                  <B4Badge
                    label={
                      familyNames[
                        domainResult.results[domainResult.best_preset].family!
                      ]
                    }
                    color="primary"
                  />
                )}
              </Typography>
            </Box>
          </Box>
          <Button
            variant="contained"
            startIcon={
              addingPreset ? (
                <CircularProgress size={18} color="inherit" />
              ) : (
                <AddIcon />
              )
            }
            onClick={(e) => {
              e.stopPropagation();
              onAddStrategy(
                domainResult.domain,
                domainResult.results[domainResult.best_preset],
              );
            }}
            disabled={
              addingPreset ||
              domainResult.baseline_works ||
              !domainResult.results[domainResult.best_preset]?.set
            }
            sx={{
              bgcolor: colors.secondary,
              color: colors.background.default,
              "&:hover": { bgcolor: colors.primary },
            }}
          >
            {t("discovery.useCurrentBest")}
          </Button>
        </Box>
      )}
      {!domainResult.best_success && (
        <Box sx={{ p: 2, bgcolor: colors.background.default }}>
          <Typography
            variant="body2"
            sx={{
              color: colors.text.secondary,
              display: "flex",
              alignItems: "center",
              gap: 1,
            }}
          >
            <CircularProgress size={14} sx={{ color: colors.text.secondary }} />
            {totalSuiteChecks > totalCount
              ? t("discovery.moreConfigsToTest", {
                  count: totalSuiteChecks - totalCount,
                })
              : t("discovery.testingConfigurations")}
          </Typography>
        </Box>
      )}

      <Collapse in={expanded}>
        <Divider sx={{ borderColor: colors.border.default }} />
        <Box sx={{ p: 2 }}>
          {successResults.length > 0 && (
            <Stack
              direction="row"
              spacing={0.5}
              flexWrap="wrap"
              gap={0.5}
              sx={{ mb: failedCount > 0 ? 1 : 0 }}
            >
              {successResults.map((result, idx) => (
                <Box
                  key={result.preset_name}
                  sx={{
                    display: "flex",
                    alignItems: "center",
                    gap: 0.5,
                  }}
                >
                  <B4Badge
                    label={`#${idx + 1} ${result.preset_name}`}
                    size="small"
                    color={
                      result.preset_name === domainResult.best_preset
                        ? "primary"
                        : "default"
                    }
                  />
                  {result.preset_name !== domainResult.best_preset &&
                    result.set && (
                      <Tooltip title={t("discovery.useThisConfig")}>
                        <IconButton
                          size="small"
                          onClick={() => {
                            onAddStrategy(domainResult.domain, result);
                          }}
                          disabled={addingPreset}
                          sx={{
                            p: 0.5,
                            bgcolor: colors.background.dark,
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
          {failedCount > 0 && (
            <Typography variant="caption" sx={{ color: colors.text.secondary }}>
              {t("discovery.failedCount", { count: failedCount })}
            </Typography>
          )}
        </Box>
      </Collapse>
    </Paper>
  );
};
