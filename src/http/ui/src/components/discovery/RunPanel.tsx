import { ReactNode, useEffect, useState } from "react";
import {
  Box,
  Button,
  CircularProgress,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  Typography,
} from "@mui/material";
import { useTranslation } from "react-i18next";
import { StopIcon } from "@b4.icons";
import { colors, typography } from "@design";
import { B4Alert, B4Badge } from "@b4.elements";
import {
  DiscoveryPhase,
  DiscoveryResult,
  DiscoverySuite,
} from "@models/discovery";
import {
  NO_BYPASS_PRESET,
  describeStrategy,
  formatDuration,
  testedCounts,
  verdictOf,
} from "@utils";

const STEPS = [
  "dns",
  "baseline",
  "strategies",
  "tuning",
  "combining",
  "confirming",
] as const;

function activeStep(
  phase: DiscoveryPhase | undefined,
  baselineDone: boolean,
): number {
  switch (phase) {
    case "dns_detection":
      return 0;
    case "cached":
      return 2;
    case "strategy_detection":
      return baselineDone ? 2 : 1;
    case "optimization":
      return 3;
    case "combination":
      return 4;
    case "confirmation":
      return 5;
    default:
      return 0;
  }
}

interface RunPanelProps {
  suite: DiscoverySuite;
  stopping: boolean;
  canStop: boolean;
  onStop: () => void;
  logLine: ReactNode;
}

export const RunPanel = ({
  suite,
  stopping,
  canStop,
  onStop,
  logLine,
}: RunPanelProps) => {
  const { t } = useTranslation();
  const [, setTick] = useState(0);

  useEffect(() => {
    const id = setInterval(() => setTick((n) => n + 1), 1000);
    return () => clearInterval(id);
  }, []);

  const results = suite.domain_discovery_results ?? {};
  const sites = suite.domains?.map((d) => d.domain) ?? Object.keys(results);
  const all = Object.values(results);
  const baselineDone = all.some((dr) => !!dr.results?.[NO_BYPASS_PRESET]);
  const dnsSeen = all.some((dr) => !!dr.dns_result);
  const active = activeStep(suite.current_phase, baselineDone);
  const dnsSkipped = active > 0 && !dnsSeen;
  const domainCount = Math.max(1, sites.length);
  const inStrategies =
    suite.current_phase === "cached" ||
    suite.current_phase === "strategy_detection";
  const perDomainTotal = Math.floor(suite.total_checks / domainCount);
  const stepProgress =
    inStrategies && active === 2 && perDomainTotal > 0
      ? t("discovery.steps.progress", {
          done: Math.min(
            Math.floor(suite.completed_checks / domainCount),
            perDomainTotal,
          ),
          total: perDomainTotal,
        })
      : "";

  const describe = (dr: DiscoveryResult | undefined): string => {
    if (!dr) return t("discovery.run.waiting");
    const counts = testedCounts(dr);
    switch (verdictOf(dr, false)) {
      case "found": {
        const set = dr.results?.[dr.best_preset]?.set;
        return t("discovery.run.found", {
          preset: dr.best_preset,
          description: set ? describeStrategy(set, t) : "",
        });
      }
      case "works_without_bypass":
        return t("discovery.run.loadsWithout");
      case "address_blocked":
        return t("discovery.run.addressBlocked");
      default:
        return counts.tested > 0
          ? t("discovery.run.tried", { count: counts.tested })
          : t("discovery.run.waiting");
    }
  };

  const badge = (dr: DiscoveryResult | undefined) => {
    const verdict = dr ? verdictOf(dr, false) : "checking";
    switch (verdict) {
      case "found": {
        const tries = dr?.confirm_tries ?? 0;
        if (tries > 0 && (dr?.confirmed ?? 0) === tries) {
          return (
            <B4Badge
              variant="outlined"
              color="success"
              label={t("discovery.status.confirmed", {
                passed: dr?.confirmed ?? 0,
                tries,
              })}
            />
          );
        }
        return (
          <B4Badge
            variant="outlined"
            color="warning"
            label={t("discovery.status.foundUnconfirmed")}
          />
        );
      }
      case "works_without_bypass":
        return (
          <B4Badge variant="outlined" label={t("discovery.status.noBypass")} />
        );
      case "address_blocked":
        return (
          <B4Badge
            variant="outlined"
            color="error"
            label={t("discovery.status.addressBlocked")}
          />
        );
      default:
        return (
          <B4Badge
            variant="outlined"
            icon={<CircularProgress size={10} sx={{ color: "inherit" }} />}
            label={t("discovery.status.checking")}
          />
        );
    }
  };

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
            {t("discovery.run.title", { count: sites.length })}
          </Typography>
          <Typography variant="caption" sx={{ color: colors.text.secondary }}>
            {t("discovery.run.elapsed", {
              duration: formatDuration(t, suite.start_time),
            })}
            {" · "}
            {t("discovery.run.tested", { count: suite.completed_checks })}
          </Typography>
        </Box>
        {canStop && (
          <Button
            variant="outlined"
            color="secondary"
            startIcon={
              stopping ? (
                <CircularProgress size={16} color="inherit" />
              ) : (
                <StopIcon />
              )
            }
            onClick={onStop}
            disabled={stopping}
            sx={{ whiteSpace: "nowrap" }}
          >
            {stopping ? t("discovery.stopping") : t("discovery.stop")}
          </Button>
        )}
      </Stack>

      {suite.source === "watchdog" && (
        <B4Alert severity="info">{t("discovery.source.watchdog")}</B4Alert>
      )}
      {suite.source === "mcp" && (
        <B4Alert severity="info">{t("discovery.source.mcp")}</B4Alert>
      )}

      <Box
        sx={{
          display: "flex",
          alignItems: "center",
          flexWrap: "wrap",
          rowGap: 1,
        }}
      >
        {STEPS.map((step, i) => {
          const done = i < active;
          const on = i === active;
          const skipped = step === "dns" && dnsSkipped;
          const color = on
            ? colors.secondary
            : done
              ? colors.text.secondary
              : colors.text.disabled;
          return (
            <Box
              key={step}
              sx={{ display: "flex", alignItems: "center", flex: "0 1 auto" }}
            >
              <Box
                sx={{
                  display: "flex",
                  alignItems: "center",
                  gap: 1,
                  color,
                  fontSize: 12,
                  fontWeight: on ? 600 : 400,
                  whiteSpace: "nowrap",
                }}
              >
                <Box
                  sx={{
                    width: 22,
                    height: 22,
                    borderRadius: "50%",
                    display: "grid",
                    placeItems: "center",
                    fontSize: 11,
                    border: `1px solid ${on ? colors.secondary : done ? colors.primary : colors.border.default}`,
                    bgcolor: on
                      ? colors.secondary
                      : done
                        ? colors.accent.primary
                        : "transparent",
                    color: on
                      ? colors.background.dark
                      : done
                        ? colors.primaryLight
                        : colors.text.disabled,
                  }}
                >
                  {done && !skipped ? "✓" : i + 1}
                </Box>
                {t(`discovery.steps.${step}`)}
                {skipped && (
                  <Box component="span" sx={{ color: colors.text.disabled }}>
                    · {t("discovery.steps.skipped")}
                  </Box>
                )}
                {on && stepProgress && (
                  <Box component="span" sx={{ fontWeight: 400 }}>
                    · {stepProgress}
                  </Box>
                )}
              </Box>
              {i < STEPS.length - 1 && (
                <Box
                  sx={{
                    width: 22,
                    height: 1,
                    mx: 1.25,
                    bgcolor: done ? colors.primary : colors.border.light,
                  }}
                />
              )}
            </Box>
          );
        })}
      </Box>

      {logLine}

      <Box sx={{ overflowX: "auto" }}>
        <Table size="small">
          <TableHead>
            <TableRow>
              <TableCell sx={{ width: "28%" }}>
                {t("discovery.run.site")}
              </TableCell>
              <TableCell>{t("discovery.run.soFar")}</TableCell>
              <TableCell sx={{ width: "22%" }} />
            </TableRow>
          </TableHead>
          <TableBody>
            {sites.map((site) => {
              const dr = results[site];
              return (
                <TableRow
                  key={site}
                  sx={{ "&:last-child td": { border: 0 } }}
                >
                  <TableCell sx={{ fontWeight: 600 }}>{site}</TableCell>
                  <TableCell sx={{ color: colors.text.secondary }}>
                    {describe(dr)}
                  </TableCell>
                  <TableCell>{badge(dr)}</TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      </Box>

      <Typography
        variant="caption"
        sx={{ ...typography.recipes.monoSmall, color: colors.text.disabled }}
      >
        {t("discovery.run.note")}
      </Typography>
    </Stack>
  );
};
