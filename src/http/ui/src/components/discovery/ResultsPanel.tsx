import { useState } from "react";
import {
  Box,
  Button,
  CircularProgress,
  Stack,
  Typography,
} from "@mui/material";
import { useTranslation } from "react-i18next";
import { AddIcon, CollapseIcon, ExpandIcon, RefreshIcon } from "@b4.icons";
import { colors, typography } from "@design";
import { B4Alert, B4Badge, B4ResultCard } from "@b4.elements";
import { DiscoverySuite } from "@models/discovery";
import {
  ApplyTarget,
  FoundGroup,
  ResultEntry,
  SiteEntry,
  alternatesFor,
  confirmationOf,
  formatDuration,
  formatSpeed,
  testedCounts,
  triesUntilFound,
} from "@utils";
import { StrategySummary } from "./StrategySummary";

interface ResultsPanelProps {
  suite: DiscoverySuite;
  entries: ResultEntry[];
  applying: boolean;
  canReset: boolean;
  onApply: (target: ApplyTarget) => void;
  onShowLog: () => void;
  onNewSearch: () => void;
}

export const ResultsPanel = ({
  suite,
  entries,
  applying,
  canReset,
  onApply,
  onShowLog,
  onNewSearch,
}: ResultsPanelProps) => {
  const { t } = useTranslation();
  const duration = formatDuration(t, suite.start_time, suite.end_time);
  const sites = suite.domains?.length ?? entries.length;

  let headline = t("discovery.results.done", { duration });
  if (suite.status === "canceled") {
    headline = t("discovery.results.stopped", { duration });
  } else if (suite.status === "failed") {
    headline = t("discovery.results.failed");
  }

  return (
    <Stack spacing={2}>
      <Stack
        direction="row"
        justifyContent="space-between"
        alignItems="flex-start"
        flexWrap="wrap"
        useFlexGap
        spacing={2}
      >
        <Box>
          <Typography sx={{ fontSize: 18, fontWeight: 600 }}>
            {headline}
          </Typography>
          <Typography variant="caption" sx={{ color: colors.text.secondary }}>
            {t("discovery.results.sites", { count: sites })}
            {" · "}
            {t("discovery.results.tested", { count: suite.completed_checks })}
            {" · "}
            {t("discovery.results.saved")}
          </Typography>
        </Box>
        <Button
          variant="outlined"
          startIcon={<RefreshIcon />}
          onClick={onNewSearch}
          disabled={!canReset}
          sx={{ whiteSpace: "nowrap" }}
        >
          {t("discovery.newSearch")}
        </Button>
      </Stack>

      <Stack spacing={1.5}>
        {entries.map((entry) =>
          entry.kind === "found" ? (
            <FoundCard
              key={entry.key}
              group={entry}
              suite={suite}
              applying={applying}
              onApply={onApply}
            />
          ) : (
            <SiteCard key={entry.key} entry={entry} onShowLog={onShowLog} />
          ),
        )}
      </Stack>
    </Stack>
  );
};

interface FoundCardProps {
  group: FoundGroup;
  suite: DiscoverySuite;
  applying: boolean;
  onApply: (target: ApplyTarget) => void;
}

const FoundCard = ({ group, suite, applying, onApply }: FoundCardProps) => {
  const { t } = useTranslation();
  const [expanded, setExpanded] = useState(false);
  const alternates = alternatesFor(group);
  const first = group.results[0];
  const counts = first ? testedCounts(first) : null;
  const tries = first ? triesUntilFound(first, group.preset) : 0;

  let badge = (
    <B4Badge
      variant="outlined"
      color="warning"
      label={t("discovery.status.notConfirmed")}
    />
  );
  if (group.confirmation && !group.unconfirmed) {
    badge = (
      <B4Badge
        variant="outlined"
        color="success"
        label={t("discovery.status.confirmed", {
          passed: group.confirmation.passed,
          tries: group.confirmation.tries,
        })}
      />
    );
  } else if (suite.status === "canceled") {
    badge = (
      <B4Badge
        variant="outlined"
        color="warning"
        label={t("discovery.status.stoppedEarly")}
      />
    );
  }

  return (
    <B4ResultCard
      status="ok"
      title={group.domains.join(", ")}
      subtitle={
        group.domains.length > 1 ? t("discovery.results.shareOne") : undefined
      }
      badge={badge}
      expanded={expanded}
      details={
        <Stack spacing={1.5}>
          {group.unconfirmed && (
            <Typography variant="body2" sx={{ color: colors.text.secondary }}>
              {t("discovery.results.unconfirmedNote")}
            </Typography>
          )}
          {alternates.length > 0 && (
            <Box>
              <Typography
                variant="caption"
                sx={{ color: colors.text.secondary, display: "block", mb: 0.5 }}
              >
                {t("discovery.results.alsoWorked")}
              </Typography>
              <Stack
                direction="row"
                flexWrap="wrap"
                useFlexGap
                spacing={1}
                alignItems="center"
              >
                {alternates.map((alt) => (
                  <Stack
                    key={alt.preset}
                    direction="row"
                    alignItems="center"
                    spacing={0.5}
                  >
                    <B4Badge
                      variant="outlined"
                      label={alt.preset}
                      sx={{ fontFamily: typography.recipes.monoSmall.fontFamily }}
                    />
                    <Button
                      size="small"
                      disabled={applying}
                      onClick={() =>
                        onApply({
                          domains: group.domains,
                          set: alt.set,
                          preset: alt.preset,
                        })
                      }
                      sx={{ textTransform: "none", minWidth: 0 }}
                    >
                      {t("discovery.results.useInstead")}
                    </Button>
                  </Stack>
                ))}
              </Stack>
            </Box>
          )}
          <Typography
            variant="caption"
            sx={{ color: colors.text.secondary, display: "block" }}
          >
            {group.results
              .map((dr) => {
                const c = confirmationOf(dr, group.preset);
                const speed = formatSpeed(dr.results?.[group.preset]?.speed ?? 0);
                const parts = [dr.domain];
                if (c) {
                  parts.push(
                    t("discovery.status.confirmed", {
                      passed: c.passed,
                      tries: c.tries,
                    }),
                  );
                }
                if (speed) parts.push(speed);
                return parts.join(" · ");
              })
              .join("   ·   ")}
          </Typography>
          {counts && (
            <Typography
              variant="caption"
              sx={{ color: colors.text.secondary, display: "block" }}
            >
              {t("discovery.results.counts", {
                worked: counts.worked,
                tested: counts.tested,
                failed: counts.failed,
              })}
            </Typography>
          )}
        </Stack>
      }
    >
      <Box
        sx={{
          display: "flex",
          gap: 2,
          alignItems: "flex-start",
          flexWrap: "wrap",
          p: 1.5,
          border: `1px solid ${colors.border.light}`,
          borderRadius: 1.5,
          bgcolor: colors.background.dark,
        }}
      >
        <Box sx={{ flex: "1 1 320px", minWidth: 0 }}>
          {group.set ? (
            <StrategySummary
              set={group.set}
              preset={group.preset}
              domains={group.domains}
              note={
                tries > 0
                  ? t("discovery.results.foundAfter", { count: tries })
                  : undefined
              }
            />
          ) : (
            <B4Alert severity="warning">{t("discovery.results.noSet")}</B4Alert>
          )}
        </Box>
        <Stack spacing={1} alignItems="flex-end" sx={{ flexShrink: 0 }}>
          <Button
            variant="contained"
            startIcon={
              applying ? (
                <CircularProgress size={18} color="inherit" />
              ) : (
                <AddIcon />
              )
            }
            disabled={applying || !group.set}
            onClick={() =>
              group.set &&
              onApply({
                domains: group.domains,
                set: group.set,
                preset: group.preset,
              })
            }
            sx={{
              bgcolor: colors.secondary,
              color: colors.background.default,
              "&:hover": { bgcolor: colors.primary },
              whiteSpace: "nowrap",
            }}
          >
            {t("discovery.results.apply")}
          </Button>
          <Button
            size="small"
            endIcon={expanded ? <CollapseIcon /> : <ExpandIcon />}
            onClick={() => setExpanded((v) => !v)}
            sx={{ textTransform: "none", color: colors.text.secondary }}
          >
            {expanded
              ? t("discovery.results.hideDetails")
              : t("discovery.results.details")}
          </Button>
        </Stack>
      </Box>
    </B4ResultCard>
  );
};

interface SiteCardProps {
  entry: SiteEntry;
  onShowLog: () => void;
}

const SiteCard = ({ entry, onShowLog }: SiteCardProps) => {
  const { t } = useTranslation();
  const counts = testedCounts(entry.result);

  switch (entry.verdict) {
    case "works_without_bypass":
      return (
        <B4ResultCard
          status="neutral"
          title={entry.domain}
          subtitle={t("discovery.results.worksWithout")}
          badge={
            <B4Badge variant="outlined" label={t("discovery.status.noBypass")} />
          }
        />
      );
    case "address_blocked":
      return (
        <B4ResultCard
          status="error"
          title={entry.domain}
          subtitle={t("discovery.results.addressBlocked")}
          badge={
            <B4Badge
              variant="outlined"
              color="error"
              label={t("discovery.status.addressBlocked")}
            />
          }
        />
      );
    default:
      return (
        <B4ResultCard
          status="warning"
          title={entry.domain}
          subtitle={t("discovery.results.nothingFound", {
            count: counts.tested,
          })}
          badge={
            <>
              <B4Badge
                variant="outlined"
                color="warning"
                label={t("discovery.status.nothingFound")}
              />
              <Button
                size="small"
                onClick={onShowLog}
                sx={{ textTransform: "none" }}
              >
                {t("discovery.run.showLog")}
              </Button>
            </>
          }
        />
      );
  }
};
