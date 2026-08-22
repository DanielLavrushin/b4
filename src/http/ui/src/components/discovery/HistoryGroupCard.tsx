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
import {
  AddIcon,
  RefreshIcon,
  ExpandIcon,
  CollapseIcon,
  DeleteIcon,
} from "@b4.icons";
import { colors } from "@design";
import { B4Badge } from "@b4.elements";
import { B4SetConfig } from "@models/config";
import { StrategyFamily, HistoryEntry } from "@models/discovery";
import { useTranslation } from "react-i18next";

interface HistoryGroupCardProps {
  family: StrategyFamily;
  familyName: string;
  entries: HistoryEntry[];
  expanded: boolean;
  onToggleExpand: () => void;
  onRetest: (urls: string[]) => void;
  onApply: (
    domains: string[],
    presetName: string,
    setConfig: B4SetConfig,
  ) => void;
  onDeleteEntry: (domain: string) => void;
  addingPreset: boolean;
  running: boolean;
  timeAgo: string;
}

export const HistoryGroupCard = ({
  familyName,
  entries,
  expanded,
  onToggleExpand,
  onRetest,
  onApply,
  onDeleteEntry,
  addingPreset,
  running,
  timeAgo,
}: HistoryGroupCardProps) => {
  const { t } = useTranslation();

  const fastestEntry = entries.reduce((a, b) =>
    a.best_speed > b.best_speed ? a : b,
  );
  const representativeSet =
    fastestEntry.results?.[fastestEntry.best_preset]?.set || null;

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
            {familyName}
          </Typography>
          <B4Badge
            label={t("discovery.badges.success")}
            size="small"
            variant="filled"
            color="primary"
          />
          <B4Badge
            label={t("discovery.grouped.domainCount", {
              count: entries.length,
            })}
            size="small"
            variant="outlined"
            color="primary"
          />
          <Typography variant="caption" sx={{ color: colors.text.secondary }}>
            {timeAgo}
          </Typography>
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
          {entries.map((entry) => (
            <B4Badge
              key={entry.domain}
              label={entry.domain}
              size="small"
              color="primary"
            />
          ))}
        </Stack>
        <Box
          sx={{
            display: "flex",
            justifyContent: "space-between",
            alignItems: "center",
          }}
        >
          <Button
            size="small"
            variant="outlined"
            startIcon={<RefreshIcon />}
            disabled={running}
            onClick={() => {
              onRetest(entries.map((e) => e.url));
            }}
            sx={{ textTransform: "none" }}
          >
            {t("discovery.history.retest")}
          </Button>
          {representativeSet && (
            <Button
              variant="contained"
              startIcon={
                addingPreset ? (
                  <CircularProgress size={18} color="inherit" />
                ) : (
                  <AddIcon />
                )
              }
              onClick={() => {
                const allDomains = entries.map((e) => e.domain);
                onApply(allDomains, familyName, representativeSet);
              }}
              disabled={addingPreset}
              sx={{
                bgcolor: colors.secondary,
                color: colors.background.default,
                "&:hover": { bgcolor: colors.primary },
              }}
            >
              {entries.length > 1
                ? t("discovery.grouped.applyAll", {
                    count: entries.length,
                  })
                : t("discovery.useThisStrategy")}
            </Button>
          )}
        </Box>
      </Box>

      <Collapse in={expanded}>
        <Divider sx={{ borderColor: colors.border.default }} />
        <Box sx={{ p: 2 }}>
          <Stack spacing={1}>
            {[...entries]
              .sort((a, b) => b.best_speed - a.best_speed)
              .map((entry) => (
                <Box
                  key={entry.domain}
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
                        {entry.domain}
                      </Typography>
                      <B4Badge
                        label={entry.best_preset}
                        size="small"
                        color="primary"
                      />
                    </Box>
                    <Box
                      sx={{
                        display: "flex",
                        alignItems: "center",
                        gap: 1,
                      }}
                    >
                      <Tooltip title={t("core.history.removeFromHistory")}>
                        <IconButton
                          size="small"
                          onClick={(e) => {
                            e.stopPropagation();
                            onDeleteEntry(entry.domain);
                          }}
                          sx={{
                            p: 0.5,
                            color: colors.text.secondary,
                            "&:hover": {
                              color: colors.text.primary,
                            },
                          }}
                        >
                          <DeleteIcon fontSize="small" />
                        </IconButton>
                      </Tooltip>
                    </Box>
                  </Box>
                </Box>
              ))}
          </Stack>
        </Box>
      </Collapse>
    </Paper>
  );
};
