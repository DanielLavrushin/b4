import { useState } from "react";
import {
  Box,
  Button,
  CircularProgress,
  Stack,
  Typography,
} from "@mui/material";
import { useTranslation } from "react-i18next";
import {
  AddIcon,
  CollapseIcon,
  ExpandIcon,
  LogsIcon,
  RefreshIcon,
} from "@b4.icons";
import { colors } from "@design";
import { B4Alert, B4Badge, B4ResultCard } from "@b4.elements";
import { B4SetConfig } from "@models/config";
import { DiscoverySuite, HistoryEntry } from "@models/discovery";
import {
  ApplyTarget,
  FoundGroup,
  ResultEntry,
  SiteEntry,
  alternatesFor,
  appliedMarks,
  checkUrlsFor,
  confirmationOf,
  formatDuration,
  formatSpeed,
  testedCounts,
  triesUntilFound,
} from "@utils";
import { AlternatesList } from "./AlternatesList";
import { StrategySummary } from "./StrategySummary";
import { SetVerdictCard, VerdictApply } from "./SetVerdictCard";

interface ResultsPanelProps {
  suite: DiscoverySuite;
  entries: ResultEntry[];
  history: HistoryEntry[];
  applying: boolean;
  canReset: boolean;
  setName?: string;
  runSet?: B4SetConfig;
  onApply: (target: ApplyTarget) => void;
  onApplyVerdict: (req: VerdictApply) => Promise<boolean>;
  onSaveSetUrls: (
    setId: string,
    urls: string[],
    removed: string[],
  ) => Promise<void>;
  onShowLog: () => void;
  onNewSearch: () => void;
}

export const ResultsPanel = ({
  suite,
  entries,
  history,
  applying,
  canReset,
  setName,
  runSet,
  onApply,
  onApplyVerdict,
  onSaveSetUrls,
  onShowLog,
  onNewSearch,
}: ResultsPanelProps) => {
  const { t } = useTranslation();
  const duration = formatDuration(t, suite.start_time, suite.end_time);
  const sites = suite.domains?.length ?? entries.length;
  const verdict = suite.set_id ? suite.set_verdict : undefined;

  let headline = t("discovery.results.done", { duration });
  if (
    suite.status === "canceled" ||
    (suite.stopped_early && !suite.stopped_covered)
  ) {
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
          {setName && (
            <Typography variant="body2" sx={{ color: colors.text.secondary }}>
              {t("discovery.results.forSet", { name: setName })}
            </Typography>
          )}
          <Typography variant="caption" sx={{ color: colors.text.secondary }}>
            {t("discovery.results.sites", { count: sites })}
            {" · "}
            {t("discovery.results.tested", { count: suite.completed_checks })}
            {" · "}
            {t("discovery.results.saved")}
          </Typography>
        </Box>
        <Stack direction="row" spacing={1}>
          <Button
            startIcon={<LogsIcon />}
            onClick={onShowLog}
            sx={{ textTransform: "none", whiteSpace: "nowrap" }}
          >
            {t("discovery.run.showLog")}
          </Button>
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
      </Stack>

      {verdict && (
        <SetVerdictCard
          suite={suite}
          verdict={verdict}
          set={runSet}
          setName={setName ?? runSet?.name ?? suite.set_id ?? ""}
          history={history}
          applying={applying}
          onApply={onApplyVerdict}
          onSaveUrls={onSaveSetUrls}
        />
      )}

      {verdict && entries.length > 0 && (
        <Typography
          variant="subtitle2"
          sx={{ color: colors.text.secondary, pt: 1 }}
        >
          {t("discovery.verdict.perAddress")}
        </Typography>
      )}

      <Stack spacing={1.5}>
        {entries.map((entry) =>
          entry.kind === "found" ? (
            <FoundCard
              key={entry.key}
              group={entry}
              suite={suite}
              history={history}
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
  history: HistoryEntry[];
  applying: boolean;
  onApply: (target: ApplyTarget) => void;
}

const FoundCard = ({
  group,
  suite,
  history,
  applying,
  onApply,
}: FoundCardProps) => {
  const { t } = useTranslation();
  const [expanded, setExpanded] = useState(false);
  const alternates = alternatesFor(group, 0);
  const applied = appliedMarks(history, group.domains);
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

  if (applied[group.preset]) {
    badge = (
      <>
        {badge}
        <B4Badge
          variant="outlined"
          color="secondary"
          label={t("discovery.results.tried")}
        />
      </>
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
          {!group.unconfirmed && suite.stopped_early && (
            <Typography variant="body2" sx={{ color: colors.text.secondary }}>
              {t("discovery.results.stoppedEarlyNote")}
            </Typography>
          )}
          <AlternatesList
            alternates={alternates}
            applied={applied}
            busy={applying}
            onUse={(alt) =>
              onApply({
                domains: group.domains,
                set: alt.set,
                preset: alt.preset,
                urls: checkUrlsFor(suite, group.domains),
                setId: suite.set_id,
              })
            }
          />
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
                urls: checkUrlsFor(suite, group.domains),
                setId: suite.set_id,
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
              : alternates.length > 0
                ? t("discovery.results.otherStrategies", {
                    count: alternates.length,
                  })
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
    case "gateway_intercepted":
      return (
        <B4ResultCard
          status="error"
          title={entry.domain}
          subtitle={t("discovery.results.gatewayIntercepted", {
            ips: (entry.result.dns_result?.gateway_ips ?? []).join(", "),
          })}
          badge={
            <B4Badge
              variant="outlined"
              color="error"
              label={t("discovery.status.gatewayIntercepted")}
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
