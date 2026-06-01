import { useState, useCallback, useEffect } from "react";
import {
  Box,
  Button,
  Stack,
  Typography,
  CircularProgress,
  IconButton,
  Tooltip,
} from "@mui/material";
import { motion, AnimatePresence } from "motion/react";
import { useTranslation, Trans } from "react-i18next";
import {
  StartIcon,
  StopIcon,
  RefreshIcon,
  SecurityIcon,
  DnsIcon,
  DomainIcon,
  NetworkIcon,
  SniIcon,
  SpeedIcon,
  ConnectionIcon,
  WarningIcon,
  HistoryIcon,
  DeleteIcon,
  ClearIcon,
  ExpandIcon,
  CollapseIcon,
} from "@b4.icons";
import { colors, spacing } from "@design";
import { B4Alert, B4Section, B4Badge } from "@b4.elements";
import { B4Card } from "@common/B4Card";
import { useDetector } from "@hooks/useDetector";
import type { DetectorTestType, DetectorHistoryEntry } from "@models/detector";

import { TestSelectionGrid } from "./TestSelectionGrid";
import { ProgressGauge } from "./ProgressGauge";
import { SummaryDashboard } from "./SummaryDashboard";
import { ResultSection } from "./ResultSection";
import { Legend } from "./Legend";
import { DNSResults } from "./results/DNSResults";
import { DNSAvailabilityResults } from "./results/DNSAvailabilityResults";
import { DomainsResults } from "./results/DomainsResults";
import { TCPResults } from "./results/TCPResults";
import { SNIResults } from "./results/SNIResults";
import { TelegramResults } from "./results/TelegramResults";
import { getTestName, statusColors } from "./constants";

function getHistoryStatusColor(entry: DetectorHistoryEntry): string {
  if (entry.status === "canceled") return statusColors.warning;
  if (entry.status === "failed") return statusColors.error;

  const hasIssues =
    (entry.dns_result &&
      (entry.dns_result.spoof_count > 0 ||
        entry.dns_result.intercept_count > 0 ||
        entry.dns_result.fakeip_count > 0)) ||
    (entry.domains_result && entry.domains_result.blocked_count > 0) ||
    (entry.tcp_result && entry.tcp_result.detected_count > 0) ||
    (entry.telegram_result && entry.telegram_result.verdict !== "ok");

  return hasIssues ? statusColors.error : statusColors.ok;
}

export const DetectorRunner = () => {
  const { t } = useTranslation();
  const {
    running,
    suiteId,
    suite,
    error,
    history,
    startDetector,
    cancelDetector,
    resetDetector,
    clearHistory,
    deleteHistoryEntry,
  } = useDetector();

  const defaultTests: Record<DetectorTestType, boolean> = {
    dns: true,
    "dns-availability": false,
    domains: true,
    tcp: true,
    sni: false,
    telegram: false,
  };

  const [selectedTests, setSelectedTests] = useState<
    Record<DetectorTestType, boolean>
  >(() => {
    try {
      const saved = localStorage.getItem("detector_selectedTests");
      if (saved) return { ...defaultTests, ...JSON.parse(saved) };
    } catch { /* ignore */ }
    return defaultTests;
  });

  useEffect(() => {
    localStorage.setItem("detector_selectedTests", JSON.stringify(selectedTests));
  }, [selectedTests]);

  const [expandedHistoryId, setExpandedHistoryId] = useState<string | null>(
    null,
  );

  const isReconnecting = suiteId && running && !suite;

  const progress = suite
    ? Math.min(
        (suite.completed_checks / Math.max(suite.total_checks, 1)) * 100,
        100,
      )
    : 0;

  const handleStart = useCallback(() => {
    const tests = (
      Object.entries(selectedTests) as [DetectorTestType, boolean][]
    )
      .filter(([, v]) => v)
      .map(([k]) => k);
    if (tests.length > 0) {
      void startDetector(tests);
    }
  }, [selectedTests, startDetector]);

  const anyTestSelected = Object.values(selectedTests).some(Boolean);
  const hasAnyResult =
    suite &&
    (suite.dns_result ||
      suite.dnsavail_result ||
      suite.domains_result ||
      suite.tcp_result ||
      suite.sni_result ||
      suite.telegram_result);

  function formatTimeAgo(dateStr: string): string {
    const date = new Date(dateStr);
    if (Number.isNaN(date.getTime()) || date.getFullYear() < 1970) return t("core.timeAgo.justNow");
    const diff = Date.now() - date.getTime();
    if (diff < 0) return t("core.timeAgo.justNow");
    const minutes = Math.floor(diff / 60000);
    if (minutes < 1) return t("core.timeAgo.justNow");
    if (minutes < 60) return t("core.timeAgo.minutesAgo", { count: minutes });
    const hours = Math.floor(minutes / 60);
    if (hours < 24) return t("core.timeAgo.hoursAgo", { count: hours });
    const days = Math.floor(hours / 24);
    return t("core.timeAgo.daysAgo", { count: days });
  }

  function getStatusLabel(status: string): string {
    if (status === "complete") return t("detector.history.complete");
    if (status === "canceled") return t("detector.history.canceled");
    return t("core.failed");
  }

  function getHistorySummary(entry: DetectorHistoryEntry): string {
    const parts: string[] = [];

    if (entry.dns_result) {
      const bad =
        entry.dns_result.spoof_count +
        entry.dns_result.intercept_count +
        entry.dns_result.fakeip_count;
      parts.push(bad > 0 ? t("detector.history.dnsIssues", { count: bad }) : t("detector.history.dnsOk"));
    }
    if (entry.dnsavail_result) {
      const r = entry.dnsavail_result;
      parts.push(
        t("detector.history.dnsAvail", {
          doh: `${r.doh_ok}/${r.doh_total}`,
          udp: `${r.udp_ok}/${r.udp_total}`,
        }),
      );
    }
    if (entry.domains_result) {
      parts.push(
        entry.domains_result.blocked_count > 0
          ? t("detector.history.domainsBlocked", { count: entry.domains_result.blocked_count })
          : t("detector.history.domainsOk"),
      );
    }
    if (entry.tcp_result) {
      parts.push(
        entry.tcp_result.detected_count > 0
          ? t("detector.history.tspuDetected", { count: entry.tcp_result.detected_count })
          : t("detector.history.tspuClean"),
      );
    }
    if (entry.sni_result) {
      parts.push(
        entry.sni_result.found_count > 0
          ? t("detector.history.sniFound", { count: entry.sni_result.found_count })
          : t("detector.history.sniNone"),
      );
    }
    if (entry.telegram_result) {
      parts.push(
        t("detector.history.telegram", {
          verdict: t(`detector.telegramVerdict.${entry.telegram_result.verdict}`),
        }),
      );
    }

    return parts.join(" / ");
  }

  return (
    <Stack spacing={3}>
      <B4Section
        title={t("detector.title")}
        description={t("detector.description")}
        icon={<SecurityIcon />}
      >
        <B4Alert icon={<SecurityIcon />}>
          <Trans i18nKey="detector.alert" />{" "}
          {t("detector.inspiredBy")}{" "}
          <a
            href="https://github.com/Runnin4ik/dpi-detector"
            target="_blank"
            rel="noopener noreferrer"
          >
            Runnin4ik/dpi-detector
          </a>{" "}
          {t("detector.project")}
        </B4Alert>

        {/* Test selection */}
        <AnimatePresence mode="wait">
          {!running && !suite && (
            <motion.div
              key="selection"
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              exit={{ opacity: 0, height: 0 }}
              transition={{ duration: 0.3 }}
            >
              <TestSelectionGrid
                selectedTests={selectedTests}
                onToggle={(test, checked) =>
                  setSelectedTests((prev) => ({ ...prev, [test]: checked }))
                }
              />
            </motion.div>
          )}
        </AnimatePresence>

        {/* Action buttons */}
        <Box sx={{ display: "flex", gap: 1 }}>
          {!running && !suite && (
            <Button
              startIcon={<StartIcon />}
              variant="contained"
              onClick={handleStart}
              disabled={!anyTestSelected}
            >
              {t("detector.actions.startDetection")}
            </Button>
          )}
          {(running || isReconnecting) && (
            <Button
              variant="outlined"
              color="secondary"
              startIcon={<StopIcon />}
              onClick={() => void cancelDetector()}
            >
              {t("core.cancel")}
            </Button>
          )}
          {suite && !running && (
            <Button
              variant="outlined"
              startIcon={<RefreshIcon />}
              onClick={resetDetector}
            >
              {t("detector.actions.newDetection")}
            </Button>
          )}
        </Box>

        {error && <B4Alert severity="error">{error}</B4Alert>}

        {isReconnecting && (
          <Box sx={{ display: "flex", alignItems: "center", gap: 2 }}>
            <CircularProgress size={20} sx={{ color: colors.secondary }} />
            <Typography variant="body2" sx={{ color: colors.text.secondary }}>
              {t("detector.actions.reconnecting")}
            </Typography>
          </Box>
        )}

        {/* Progress gauge */}
        <AnimatePresence>
          {running && suite && (
            <motion.div
              key="progress"
              initial={{ opacity: 0, scale: 0.95 }}
              animate={{ opacity: 1, scale: 1 }}
              exit={{ opacity: 0, scale: 0.95 }}
              transition={{ duration: 0.3 }}
            >
              <ProgressGauge
                progress={progress}
                currentTest={suite.current_test}
                completedChecks={suite.completed_checks}
                totalChecks={suite.total_checks}
                tests={suite.tests}
                suite={suite}
              />
            </motion.div>
          )}
        </AnimatePresence>
      </B4Section>

      {/* Summary Dashboard */}
      <AnimatePresence>
        {hasAnyResult && (
          <motion.div
            key="summary"
            initial={{ opacity: 0, y: 20 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.4 }}
          >
            <SummaryDashboard suite={suite} />
          </motion.div>
        )}
      </AnimatePresence>

      {/* Result sections in flexible layout */}
      {hasAnyResult && (
        <Box sx={{ display: "flex", flexWrap: "wrap", gap: 3 }}>
          {suite?.dns_result && (
            <Box sx={{ flex: "1 1 480px", minWidth: 0 }}>
              <ResultSection
                title={t("detector.sections.dnsIntegrityCheck")}
                icon={<DnsIcon />}
                summary={suite.dns_result.summary}
                ok={suite.dns_result.status === "OK"}
              >
                {suite.dns_result.doh_blocked && (
                  <B4Alert severity="error">
                    {t("detector.alerts.dohBlocked")}
                  </B4Alert>
                )}
                {suite.dns_result.udp_blocked && (
                  <B4Alert severity="error">
                    {t("detector.alerts.udpBlocked")}
                  </B4Alert>
                )}
                {suite.dns_result.stub_ips &&
                  suite.dns_result.stub_ips.length > 0 && (
                    <B4Alert severity="warning" icon={<WarningIcon />}>
                      {t("detector.alerts.stubIps", { ips: suite.dns_result.stub_ips.join(", ") })}
                    </B4Alert>
                  )}
                {suite.dns_result.domains && suite.dns_result.domains.length > 0 && (
                  <DNSResults domains={suite.dns_result.domains} />
                )}
              </ResultSection>
            </Box>
          )}

          {suite?.dnsavail_result && (
            <Box sx={{ flex: "1 1 480px", minWidth: 0 }}>
              <ResultSection
                title={t("detector.sections.dnsAvailability")}
                icon={<SpeedIcon />}
                summary={suite.dnsavail_result.summary}
                ok={
                  suite.dnsavail_result.doh_ok > 0 ||
                  suite.dnsavail_result.udp_ok > 0
                }
              >
                {suite.dnsavail_result.providers &&
                  suite.dnsavail_result.providers.length > 0 && (
                    <DNSAvailabilityResults
                      providers={suite.dnsavail_result.providers}
                    />
                  )}
              </ResultSection>
            </Box>
          )}

          {suite?.domains_result && (
            <Box sx={{ flex: "2 1 480px", minWidth: 0 }}>
              <ResultSection
                title={t("detector.sections.domainAccessibility")}
                icon={<DomainIcon />}
                summary={suite.domains_result.summary}
                ok={suite.domains_result.blocked_count === 0}
              >
                {suite.domains_result.domains &&
                  suite.domains_result.domains.length > 0 && (
                    <DomainsResults domains={suite.domains_result.domains} />
                  )}
              </ResultSection>
            </Box>
          )}

          {suite?.tcp_result && (
            <Box sx={{ flex: "2 1 480px", minWidth: 0 }}>
              <ResultSection
                title={t("detector.sections.tcpFatProbeTest")}
                icon={<NetworkIcon />}
                summary={suite.tcp_result.summary}
                ok={suite.tcp_result.detected_count === 0}
              >
                {suite.tcp_result.targets && suite.tcp_result.targets.length > 0 && (
                  <TCPResults targets={suite.tcp_result.targets} />
                )}
              </ResultSection>
            </Box>
          )}

          {suite?.sni_result && (
            <Box sx={{ flex: "1 1 480px", minWidth: 0 }}>
              <ResultSection
                title={t("detector.sections.sniWhitelistBruteForce")}
                icon={<SniIcon />}
                summary={suite.sni_result.summary}
                ok={
                  suite.sni_result.tested_count === 0 ||
                  suite.sni_result.found_count > 0
                }
              >
                {suite.sni_result.asn_results &&
                  suite.sni_result.asn_results.length > 0 && (
                    <SNIResults results={suite.sni_result.asn_results} />
                  )}
              </ResultSection>
            </Box>
          )}

          {suite?.telegram_result && (
            <Box sx={{ flex: "2 1 480px", minWidth: 0 }}>
              <ResultSection
                title={t("detector.sections.telegram")}
                icon={<ConnectionIcon />}
                summary={suite.telegram_result.summary}
                ok={suite.telegram_result.verdict === "ok"}
              >
                <TelegramResults result={suite.telegram_result} />
              </ResultSection>
            </Box>
          )}
        </Box>
      )}

      <Legend />

      {/* Detection History */}
      {history.length > 0 && (
        <B4Section
          title={t("core.history.title")}
          description={t("detector.history.detectionsSaved", { count: history.length })}
          icon={<HistoryIcon />}
        >
          <Box sx={{ display: "flex", justifyContent: "flex-end", mb: 1 }}>
            <Button
              size="small"
              startIcon={<ClearIcon />}
              onClick={() => void clearHistory()}
              sx={{ color: colors.text.secondary }}
            >
              {t("core.history.clearHistory")}
            </Button>
          </Box>
          <Stack spacing={1}>
            {history.map((entry) => {
              const isExpanded = expandedHistoryId === entry.id;
              const color = getHistoryStatusColor(entry);
              const summaryText = getHistorySummary(entry);

              return (
                <motion.div
                  key={entry.id}
                  initial={{ opacity: 0, y: 10 }}
                  animate={{ opacity: 1, y: 0 }}
                  transition={{ duration: 0.2 }}
                >
                  <B4Card
                    variant="outlined"
                    sx={{
                      borderLeft: `3px solid ${color}`,
                      overflow: "hidden",
                    }}
                  >
                    {/* Header */}
                    <Box
                      sx={{
                        p: spacing.md,
                        cursor: "pointer",
                        "&:hover": { bgcolor: `${colors.text.primary}08` },
                      }}
                      onClick={() =>
                        setExpandedHistoryId(isExpanded ? null : entry.id)
                      }
                    >
                      <Stack
                        direction="row"
                        alignItems="center"
                        justifyContent="space-between"
                      >
                        <Stack
                          direction="row"
                          alignItems="center"
                          spacing={1.5}
                          sx={{ flex: 1, minWidth: 0 }}
                        >
                          {isExpanded ? (
                            <CollapseIcon
                              sx={{
                                fontSize: 20,
                                color: colors.text.secondary,
                              }}
                            />
                          ) : (
                            <ExpandIcon
                              sx={{
                                fontSize: 20,
                                color: colors.text.secondary,
                              }}
                            />
                          )}
                          <Stack spacing={0.25} sx={{ minWidth: 0 }}>
                            <Stack
                              direction="row"
                              alignItems="center"
                              spacing={1}
                            >
                              <B4Badge
                                label={getStatusLabel(entry.status)}
                                sx={{
                                  bgcolor: `${color}22`,
                                  color,
                                  fontWeight: 600,
                                  fontSize: "0.7rem",
                                }}
                                size="small"
                              />
                              {entry.tests.map((test) => (
                                <B4Badge
                                  key={test}
                                  label={getTestName(t, test)}
                                  size="small"
                                  sx={{
                                    bgcolor: `${colors.text.primary}11`,
                                    color: colors.text.secondary,
                                    fontSize: "0.65rem",
                                  }}
                                />
                              ))}
                            </Stack>
                            <Typography
                              variant="caption"
                              sx={{
                                color: colors.text.secondary,
                                overflow: "hidden",
                                textOverflow: "ellipsis",
                                whiteSpace: "nowrap",
                              }}
                            >
                              {summaryText}
                            </Typography>
                          </Stack>
                        </Stack>
                        <Stack
                          direction="row"
                          alignItems="center"
                          spacing={1}
                        >
                          <Typography
                            variant="caption"
                            sx={{ color: colors.text.secondary }}
                          >
                            {formatTimeAgo(entry.end_time)}
                          </Typography>
                          <Tooltip title={t("core.history.removeFromHistory")}>
                            <IconButton
                              size="small"
                              onClick={(e) => {
                                e.stopPropagation();
                                void deleteHistoryEntry(entry.id);
                              }}
                              sx={{ color: colors.text.secondary }}
                            >
                              <DeleteIcon fontSize="small" />
                            </IconButton>
                          </Tooltip>
                        </Stack>
                      </Stack>
                    </Box>

                    {/* Expanded details */}
                    <AnimatePresence>
                      {isExpanded && (
                        <motion.div
                          initial={{ height: 0, opacity: 0 }}
                          animate={{ height: "auto", opacity: 1 }}
                          exit={{ height: 0, opacity: 0 }}
                          transition={{ duration: 0.25 }}
                          style={{ overflow: "hidden" }}
                        >
                          <Stack
                            spacing={2}
                            sx={{
                              px: spacing.md,
                              pb: spacing.md,
                              borderTop: `1px solid ${colors.text.primary}11`,
                              pt: spacing.md,
                            }}
                          >
                            {/* DNS */}
                            {entry.dns_result && (
                              <ResultSection
                                title={t("detector.sections.dnsIntegrityCheck")}
                                icon={<DnsIcon />}
                                summary={entry.dns_result.summary}
                                ok={entry.dns_result.status === "OK"}
                              >
                                {entry.dns_result.domains &&
                                  entry.dns_result.domains.length > 0 && (
                                    <DNSResults
                                      domains={entry.dns_result.domains}
                                    />
                                  )}
                              </ResultSection>
                            )}

                            {/* DNS availability */}
                            {entry.dnsavail_result && (
                              <ResultSection
                                title={t("detector.sections.dnsAvailability")}
                                icon={<SpeedIcon />}
                                summary={entry.dnsavail_result.summary}
                                ok={
                                  entry.dnsavail_result.doh_ok > 0 ||
                                  entry.dnsavail_result.udp_ok > 0
                                }
                              >
                                {entry.dnsavail_result.providers &&
                                  entry.dnsavail_result.providers.length > 0 && (
                                    <DNSAvailabilityResults
                                      providers={entry.dnsavail_result.providers}
                                    />
                                  )}
                              </ResultSection>
                            )}

                            {/* Domains */}
                            {entry.domains_result && (
                              <ResultSection
                                title={t("detector.sections.domainAccessibility")}
                                icon={<DomainIcon />}
                                summary={entry.domains_result.summary}
                                ok={
                                  entry.domains_result.blocked_count === 0
                                }
                              >
                                {entry.domains_result.domains &&
                                  entry.domains_result.domains.length > 0 && (
                                    <DomainsResults
                                      domains={entry.domains_result.domains}
                                    />
                                  )}
                              </ResultSection>
                            )}

                            {/* TCP */}
                            {entry.tcp_result && (
                              <ResultSection
                                title={t("detector.sections.tcpFatProbeTest")}
                                icon={<NetworkIcon />}
                                summary={entry.tcp_result.summary}
                                ok={entry.tcp_result.detected_count === 0}
                              >
                                {entry.tcp_result.targets &&
                                  entry.tcp_result.targets.length > 0 && (
                                    <TCPResults
                                      targets={entry.tcp_result.targets}
                                    />
                                  )}
                              </ResultSection>
                            )}

                            {/* SNI */}
                            {entry.sni_result && (
                              <ResultSection
                                title={t("detector.sections.sniWhitelistBruteForce")}
                                icon={<SniIcon />}
                                summary={entry.sni_result.summary}
                                ok={
                                  entry.sni_result.tested_count === 0 ||
                                  entry.sni_result.found_count > 0
                                }
                              >
                                {entry.sni_result.asn_results &&
                                  entry.sni_result.asn_results.length > 0 && (
                                    <SNIResults
                                      results={entry.sni_result.asn_results}
                                    />
                                  )}
                              </ResultSection>
                            )}

                            {/* Telegram */}
                            {entry.telegram_result && (
                              <ResultSection
                                title={t("detector.sections.telegram")}
                                icon={<ConnectionIcon />}
                                summary={entry.telegram_result.summary}
                                ok={entry.telegram_result.verdict === "ok"}
                              >
                                <TelegramResults
                                  result={entry.telegram_result}
                                />
                              </ResultSection>
                            )}
                          </Stack>
                        </motion.div>
                      )}
                    </AnimatePresence>
                  </B4Card>
                </motion.div>
              );
            })}
          </Stack>
        </B4Section>
      )}
    </Stack>
  );
};
