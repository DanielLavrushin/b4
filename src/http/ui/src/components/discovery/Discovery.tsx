import { useState, useRef, useCallback, useEffect } from "react";
import {
  Box,
  Button,
  Stack,
  Typography,
  LinearProgress,
  CircularProgress,
} from "@mui/material";
import {
  StartIcon,
  StopIcon,
  RefreshIcon,
  DiscoveryIcon,
  HistoryIcon,
  ClearIcon,
} from "@b4.icons";
import { colors } from "@design";
import { B4SetConfig } from "@models/config";
import { DiscoveryAddDialog } from "./AddDialog";
import {
  B4Alert,
  B4Badge,
  B4Section,
  B4TextField,
  B4ChipList,
  B4PlusButton,
} from "@b4.elements";
import { useSnackbar } from "@context/SnackbarProvider";
import { DiscoveryLogPanel } from "./LogPanel";
import { useDiscovery } from "@hooks/useDiscovery";
import {
  StrategyFamily,
  DiscoveryPhase,
  DomainPresetResult,
  HistoryEntry,
} from "@models/discovery";
import { useSets } from "@hooks/useSets";
import { useCaptures } from "@b4.capture";
import { DiscoveryOptionsPanel, DiscoveryOptions } from "./Options";
import { useTranslation, Trans } from "react-i18next";
import {
  groupByStrategy,
  formatTimeAgo,
  StrategyGroup,
} from "../../utils/discovery";
import { RunningDomainCard } from "./RunningDomainCard";
import { StrategyGroupCard } from "./StrategyGroupCard";
import { FailedDomainCard } from "./FailedDomainCard";
import { HistoryGroupCard } from "./HistoryGroupCard";
import { configApi } from "@b4.settings";

export const DiscoveryRunner = () => {
  const { t } = useTranslation();

  const familyNames: Record<StrategyFamily, string> = {
    none: t("discovery.familyNames.none"),
    tcp_frag: t("discovery.familyNames.tcp_frag"),
    tls_record: t("discovery.familyNames.tls_record"),
    oob: t("discovery.familyNames.oob"),
    ip_frag: t("discovery.familyNames.ip_frag"),
    fake_sni: t("discovery.familyNames.fake_sni"),
    sack: t("discovery.familyNames.sack"),
    syn_fake: t("discovery.familyNames.syn_fake"),
    desync: t("discovery.familyNames.desync"),
    delay: t("discovery.familyNames.delay"),
    disorder: t("discovery.familyNames.disorder"),
    extsplit: t("discovery.familyNames.extsplit"),
    firstbyte: t("discovery.familyNames.firstbyte"),
    combo: t("discovery.familyNames.combo"),
    hybrid: t("discovery.familyNames.hybrid"),
    window: t("discovery.familyNames.window"),
    mutation: t("discovery.familyNames.mutation"),
    incoming: t("discovery.familyNames.incoming"),
  };

  const phaseNames: Record<DiscoveryPhase, string> = {
    baseline: t("discovery.phaseNames.baseline"),
    cached: t("discovery.phaseNames.cached"),
    strategy_detection: t("discovery.phaseNames.strategy_detection"),
    optimization: t("discovery.phaseNames.optimization"),
    combination: t("discovery.phaseNames.combination"),
    dns_detection: t("discovery.phaseNames.dns_detection"),
  };

  const {
    startDiscovery,
    cancelDiscovery,
    resetDiscovery,
    addPresetAsSet,
    clearCache,
    clearHistory,
    deleteHistoryDomain,
    discoveryRunning: running,
    suiteId,
    suite,
    error,
    history,
  } = useDiscovery();
  const { showSuccess, showError } = useSnackbar();

  const { addDomainToSet } = useSets();

  const [expandedDomains, setExpandedDomains] = useState<Set<string>>(
    new Set(),
  );
  const [expandedHistoryDomains, setExpandedHistoryDomains] = useState<
    Set<string>
  >(new Set());

  const { captures, loadCaptures } = useCaptures();
  const [options, setOptions] = useState<DiscoveryOptions>(() => ({
    skipDNS: localStorage.getItem("b4_discovery_skipdns") === "true",
    skipCache: localStorage.getItem("b4_discovery_skipcache") === "true",
    payloadFiles: [],
    validationTries:
      Number(localStorage.getItem("b4_discovery_validation_tries")) || 1,
    tlsVersion:
      (localStorage.getItem(
        "b4_discovery_tls_version",
      ) as DiscoveryOptions["tlsVersion"]) || "auto",
    ipVersion:
      (localStorage.getItem(
        "b4_discovery_ip_version",
      ) as DiscoveryOptions["ipVersion"]) || "auto",
  }));

  useEffect(() => {
    void loadCaptures();
  }, [loadCaptures]);

  useEffect(() => {
    localStorage.setItem("b4_discovery_skipdns", String(options.skipDNS));
  }, [options.skipDNS]);

  useEffect(() => {
    localStorage.setItem("b4_discovery_skipcache", String(options.skipCache));
  }, [options.skipCache]);

  useEffect(() => {
    localStorage.setItem(
      "b4_discovery_validation_tries",
      String(options.validationTries),
    );
  }, [options.validationTries]);

  useEffect(() => {
    localStorage.setItem("b4_discovery_tls_version", options.tlsVersion);
  }, [options.tlsVersion]);

  useEffect(() => {
    localStorage.setItem("b4_discovery_ip_version", options.ipVersion);
  }, [options.ipVersion]);

  const [ipVersionEnabled, setIpVersionEnabled] = useState(true);
  useEffect(() => {
    void configApi
      .get()
      .then((c) => setIpVersionEnabled(!!c.queue?.ipv4 && !!c.queue?.ipv6))
      .catch(() => {});
  }, []);

  const effectiveIpVersion = ipVersionEnabled ? options.ipVersion : "auto";

  const [checkUrls, setCheckUrls] = useState<string[]>([]);
  const [urlInput, setUrlInput] = useState("");

  const [addingPreset, setAddingPreset] = useState(false);
  const [addDialog, setAddDialog] = useState<{
    open: boolean;
    domain: string;
    domains: string[];
    presetName: string;
    setConfig: B4SetConfig | null;
  }>({ open: false, domain: "", domains: [], presetName: "", setConfig: null });
  const domainInputRef = useRef<HTMLInputElement | null>(null);

  const progress = suite
    ? Math.min((suite.completed_checks / suite.total_checks) * 100, 100)
    : 0;
  const isReconnecting = suiteId && running && !suite;

  useEffect(() => {
    void loadCaptures();
  }, [loadCaptures]);

  const handleAddStrategy = (domain: string, result: DomainPresetResult) => {
    let presetName = result.preset_name;
    if (options.tlsVersion === "tls12") presetName += "-tls12";
    else if (options.tlsVersion === "tls13") presetName += "-tls13";
    if (effectiveIpVersion === "ipv4") presetName += "-ipv4";
    else if (effectiveIpVersion === "ipv6") presetName += "-ipv6";

    setAddDialog({
      open: true,
      domain,
      domains: [domain],
      presetName,
      setConfig: result.set || null,
    });
  };

  const handleAddGroupStrategy = (group: StrategyGroup) => {
    let presetName = group.winnerPreset || familyNames[group.family];
    if (options.tlsVersion === "tls12") presetName += "-tls12";
    else if (options.tlsVersion === "tls13") presetName += "-tls13";
    if (effectiveIpVersion === "ipv4") presetName += "-ipv4";
    else if (effectiveIpVersion === "ipv6") presetName += "-ipv6";

    setAddDialog({
      open: true,
      domain: group.domains[0].domain,
      domains: group.domains.map((d) => d.domain),
      presetName,
      setConfig: group.representativeSet,
    });
  };

  const toggleDomainExpand = (domain: string) => {
    setExpandedDomains((prev) => {
      const next = new Set(prev);
      if (next.has(domain)) {
        next.delete(domain);
      } else {
        next.add(domain);
      }
      return next;
    });
  };

  const extractDomain = (url: string): string => {
    try {
      const withProto = url.includes("://") ? url : `https://${url}`;
      return new URL(withProto).hostname;
    } catch {
      return url.split("/")[0];
    }
  };

  const addUrls = useCallback((raw: string) => {
    const parts = raw
      .split(/[\n,]+/)
      .map((l) =>
        l
          .trim()
          .replace(/^["'`]+|["'`]+$/g, "")
          .trim(),
      )
      .filter((l) => l.length > 0);
    if (parts.length === 0) return;
    setCheckUrls((prev) => {
      const existing = new Set(prev);
      const next = [...prev];
      for (const url of parts) {
        if (!existing.has(url)) {
          existing.add(url);
          next.push(url);
        }
      }
      return next;
    });
    setUrlInput("");
  }, []);

  const handleUrlKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLInputElement>) => {
      if (e.key === "Enter" || e.key === "Tab" || e.key === ",") {
        if (urlInput.trim()) {
          e.preventDefault();
          addUrls(urlInput);
        }
      }
    },
    [urlInput, addUrls],
  );

  const handleUrlPaste = useCallback(
    (e: React.ClipboardEvent) => {
      const text = e.clipboardData.getData("text");
      if (text.includes("\n") || text.includes(",")) {
        e.preventDefault();
        addUrls(text);
      }
    },
    [addUrls],
  );

  const removeUrl = useCallback((url: string) => {
    setCheckUrls((prev) => prev.filter((u) => u !== url));
  }, []);

  const handleAddNew = async (
    name: string,
    domain: string,
    allDomains?: string[],
  ) => {
    if (!addDialog.setConfig) return;
    setAddingPreset(true);
    const sniDomains =
      allDomains && allDomains.length > 1 ? allDomains : [domain];
    const configToAdd = {
      ...addDialog.setConfig,
      name,
      targets: { ...addDialog.setConfig.targets, sni_domains: sniDomains },
    };
    const res = await addPresetAsSet(configToAdd);
    if (res.success) {
      showSuccess(t("discovery.createdSet", { name }));
      setAddDialog({
        open: false,
        domain: "",
        domains: [],
        presetName: "",
        setConfig: null,
      });
    } else {
      showError(t("discovery.createSetFailed"));
    }
    setAddingPreset(false);
  };

  const handleAddToExisting = async (
    setId: string,
    domain: string,
    allDomains?: string[],
  ) => {
    setAddingPreset(true);
    const domainsToAdd =
      allDomains && allDomains.length > 1 ? allDomains : [domain];
    let allSuccess = true;
    for (const d of domainsToAdd) {
      const res = await addDomainToSet(setId, d);
      if (!res.success) {
        allSuccess = false;
        break;
      }
    }
    if (allSuccess) {
      showSuccess(t("discovery.addedDomainToSet"));
      setAddDialog({
        open: false,
        domain: "",
        domains: [],
        presetName: "",
        setConfig: null,
      });
    } else {
      showError(t("discovery.addDomainToSetFailed"));
    }
    setAddingPreset(false);
  };

  const handleReset = useCallback(() => {
    resetDiscovery();
    setExpandedDomains(new Set());
  }, [resetDiscovery]);

  const handleHistoryApply = (
    domains: string[],
    presetName: string,
    setConfig: B4SetConfig,
  ) => {
    setAddDialog({
      open: true,
      domain: domains[0],
      domains,
      presetName,
      setConfig,
    });
  };

  const handleHistoryDelete = (domain: string) => {
    void (async () => {
      const res = await deleteHistoryDomain(domain);
      if (res.success)
        showSuccess(t("discovery.history.removedFromHistory", { domain }));
    })();
  };

  const handleHistoryRetest = (urls: string[]) => {
    setCheckUrls(urls);
    resetDiscovery();
  };

  return (
    <Stack spacing={3}>
      <B4Section
        title={t("discovery.title")}
        description={t("discovery.description")}
        icon={<DiscoveryIcon />}
      >
        <B4Alert icon={<DiscoveryIcon />}>
          <Trans i18nKey="discovery.alert" />
        </B4Alert>

        <Box sx={{ display: "flex", gap: 1, alignItems: "flex-start" }}>
          <B4TextField
            label={t("discovery.addDomainLabel")}
            value={urlInput}
            onChange={(e) => setUrlInput(e.target.value)}
            onKeyDown={handleUrlKeyDown}
            onPaste={handleUrlPaste}
            inputRef={domainInputRef}
            placeholder={t("discovery.addDomainPlaceholder")}
            disabled={running || !!isReconnecting}
            helperText={t("discovery.addDomainHelper")}
          />
          <B4PlusButton
            onClick={() => addUrls(urlInput)}
            disabled={!urlInput.trim() || running || !!isReconnecting}
          />
          <Box sx={{ flexShrink: 0 }}>
            {!running && !suite && (
              <Button
                startIcon={<StartIcon />}
                variant="contained"
                onClick={() => {
                  void startDiscovery(
                    checkUrls,
                    options.skipDNS,
                    options.skipCache,
                    options.payloadFiles,
                    options.validationTries,
                    options.tlsVersion,
                    effectiveIpVersion,
                  );
                }}
                disabled={checkUrls.length === 0}
                sx={{
                  whiteSpace: "nowrap",
                }}
              >
                {t("discovery.startDiscovery")}
              </Button>
            )}
            {(running || isReconnecting) && (
              <Button
                variant="outlined"
                color="secondary"
                startIcon={<StopIcon />}
                onClick={() => {
                  void cancelDiscovery();
                }}
                sx={{
                  whiteSpace: "nowrap",
                }}
              >
                {t("core.cancel")}
              </Button>
            )}
            {suite && !running && (
              <Button
                variant="outlined"
                startIcon={<RefreshIcon />}
                onClick={handleReset}
                sx={{
                  whiteSpace: "nowrap",
                }}
              >
                {t("discovery.newDiscovery")}
              </Button>
            )}
          </Box>
        </Box>
        {!running && !suite && (
          <B4ChipList
            items={checkUrls}
            getKey={(url) => url}
            getLabel={(url) => extractDomain(url)}
            onDelete={removeUrl}
            emptyMessage={t("discovery.noUrlsAdded")}
            showEmpty
          />
        )}
        <DiscoveryOptionsPanel
          options={options}
          ipVersionEnabled={ipVersionEnabled}
          onChange={setOptions}
          onClearCache={() => {
            void (async () => {
              const res = await clearCache();
              if (res.success) showSuccess(t("discovery.cacheCleared"));
              else showError(t("discovery.cacheClearFailed"));
            })();
          }}
          captures={captures}
          disabled={running || !!isReconnecting}
        />
        {error && <B4Alert severity="error">{error}</B4Alert>}

        {isReconnecting && (
          <Box sx={{ display: "flex", alignItems: "center", gap: 2 }}>
            <CircularProgress size={20} sx={{ color: colors.secondary }} />
            <Typography variant="body2" sx={{ color: colors.text.secondary }}>
              {t("discovery.reconnecting")}
            </Typography>
          </Box>
        )}
        {running && suite && (
          <Box>
            <Box
              sx={{ display: "flex", justifyContent: "space-between", mb: 1 }}
            >
              <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
                <Typography variant="body2" color="text.secondary">
                  {suite.current_phase && (
                    <B4Badge
                      label={phaseNames[suite.current_phase]}
                      size="small"
                      color="primary"
                      sx={{ mr: 2 }}
                    />
                  )}
                  {suite.current_phase === "dns_detection"
                    ? t("discovery.checkingDns")
                    : t("discovery.checksProgress", {
                        completed: suite.completed_checks,
                        total: suite.total_checks,
                      })}
                  {suite.current_domain && (
                    <B4Badge
                      label={suite.current_domain}
                      size="small"
                      variant="outlined"
                      color="primary"
                      sx={{ ml: 1 }}
                    />
                  )}
                </Typography>
              </Box>
              {suite.current_phase !== "dns_detection" && (
                <Typography variant="body2" color="text.secondary">
                  {Number.isNaN(progress) ? "0" : progress.toFixed(0)}%
                </Typography>
              )}
            </Box>
            <LinearProgress
              variant={
                suite.current_phase === "dns_detection"
                  ? "indeterminate"
                  : "determinate"
              }
              value={progress}
              sx={{
                height: 8,
                borderRadius: 4,
                bgcolor: colors.background.dark,
                "& .MuiLinearProgress-bar": {
                  bgcolor: colors.secondary,
                  borderRadius: 4,
                },
              }}
            />
          </Box>
        )}
        {suite && (
          <Box sx={{ mt: running ? 3 : 0 }}>
            <DiscoveryLogPanel running={running} />
          </Box>
        )}
      </B4Section>

      {running &&
        suite?.domain_discovery_results &&
        Object.keys(suite.domain_discovery_results).length > 0 && (
          <Box
            sx={{
              display: "grid",
              gridTemplateColumns: {
                xs: "1fr",
                md: "1fr 1fr",
                xl: "1fr 1fr 1fr",
              },
              gap: 2,
            }}
          >
            {Object.values(suite.domain_discovery_results)
              .sort((a, b) => b.best_speed - a.best_speed)
              .map((domainResult) => (
                <RunningDomainCard
                  key={domainResult.domain}
                  domainResult={domainResult}
                  expanded={expandedDomains.has(domainResult.domain)}
                  onToggleExpand={() => toggleDomainExpand(domainResult.domain)}
                  onAddStrategy={handleAddStrategy}
                  addingPreset={addingPreset}
                  familyNames={familyNames}
                  totalSuiteChecks={suite.total_checks}
                  running={running}
                />
              ))}
          </Box>
        )}

      {!running &&
        suite?.domain_discovery_results &&
        Object.keys(suite.domain_discovery_results).length > 0 &&
        (() => {
          const { success: strategyGroups, failed: failedDomains } =
            groupByStrategy(
              suite.domain_discovery_results,
              suite.strategy_groups,
            );
          return (
            <Box
              sx={{
                display: "grid",
                gridTemplateColumns: {
                  xs: "1fr",
                  md: "1fr 1fr",
                  xl: "1fr 1fr 1fr",
                },
                gap: 2,
              }}
            >
              {strategyGroups.map((group) => {
                const groupKey = `${group.family}::${group.winnerPreset}`;
                return (
                  <StrategyGroupCard
                    key={groupKey}
                    group={group}
                    expanded={expandedDomains.has(groupKey)}
                    onToggleExpand={() => toggleDomainExpand(groupKey)}
                    onApply={() => handleAddGroupStrategy(group)}
                    addingPreset={addingPreset}
                    familyNames={familyNames}
                    domainResults={suite.domain_discovery_results}
                  />
                );
              })}

              {failedDomains.map((dr) => (
                <FailedDomainCard
                  key={dr.domain}
                  domain={dr.domain}
                  transportBlocked={dr.dns_result?.transport_blocked}
                  resultsCount={Object.keys(dr.results).length}
                />
              ))}
            </Box>
          );
        })()}

      {history.length > 0 && (
        <B4Section
          title={t("core.history.title")}
          description={
            history.length === 1
              ? t("discovery.history.domainsTested", { count: history.length })
              : t("discovery.history.domainsTested_plural", {
                  count: history.length,
                })
          }
          icon={<HistoryIcon />}
        >
          <Box sx={{ display: "flex", justifyContent: "flex-end", mt: -1 }}>
            <Button
              size="small"
              startIcon={<ClearIcon />}
              onClick={() => {
                void (async () => {
                  const res = await clearHistory();
                  if (res.success)
                    showSuccess(t("discovery.history.historyCleared"));
                  else showError(t("discovery.history.historyClearFailed"));
                })();
              }}
              sx={{ color: colors.text.secondary, textTransform: "none" }}
            >
              {t("core.history.clearHistory")}
            </Button>
          </Box>
          {(() => {
            const sorted = [...history].sort(
              (a: HistoryEntry, b: HistoryEntry) =>
                new Date(b.end_time).getTime() - new Date(a.end_time).getTime(),
            );
            const familyGroups: Record<string, HistoryEntry[]> = {};
            const failedEntries: HistoryEntry[] = [];
            sorted.forEach((entry) => {
              if (!entry.best_success) {
                failedEntries.push(entry);
                return;
              }
              const family = entry.best_family || "none";
              if (!familyGroups[family]) familyGroups[family] = [];
              familyGroups[family].push(entry);
            });

            const groupEntries = Object.entries(familyGroups).sort(
              ([, a], [, b]) =>
                Math.max(...b.map((e) => e.best_speed)) -
                Math.max(...a.map((e) => e.best_speed)),
            );

            return (
              <Box
                sx={{
                  display: "grid",
                  gridTemplateColumns: {
                    xs: "1fr",
                    md: "1fr 1fr",
                    xl: "1fr 1fr 1fr",
                  },
                  gap: 2,
                }}
              >
                {groupEntries.map(([family, entries]) => {
                  const familyKey = family as StrategyFamily;
                  return (
                    <HistoryGroupCard
                      key={family}
                      family={familyKey}
                      familyName={familyNames[familyKey] ?? family}
                      entries={entries}
                      expanded={expandedHistoryDomains.has(family)}
                      onToggleExpand={() => {
                        setExpandedHistoryDomains((prev) => {
                          const next = new Set(prev);
                          if (next.has(family)) next.delete(family);
                          else next.add(family);
                          return next;
                        });
                      }}
                      onRetest={handleHistoryRetest}
                      onApply={handleHistoryApply}
                      onDeleteEntry={handleHistoryDelete}
                      addingPreset={addingPreset}
                      running={running}
                      timeAgo={formatTimeAgo(
                        t,
                        entries[0].end_time,
                        entries[0].start_time,
                      )}
                    />
                  );
                })}

                {failedEntries.map((entry) => (
                  <FailedDomainCard
                    key={entry.domain}
                    domain={entry.domain}
                    transportBlocked={entry.dns_result?.transport_blocked}
                    resultsCount={Object.keys(entry.results || {}).length}
                    timeAgo={formatTimeAgo(t, entry.end_time, entry.start_time)}
                    onDelete={() => handleHistoryDelete(entry.domain)}
                  />
                ))}
              </Box>
            );
          })()}
        </B4Section>
      )}

      <DiscoveryAddDialog
        open={addDialog.open}
        domain={addDialog.domain}
        domains={addDialog.domains}
        presetName={addDialog.presetName}
        setConfig={addDialog.setConfig}
        onClose={() =>
          setAddDialog({
            open: false,
            domain: "",
            domains: [],
            presetName: "",
            setConfig: null,
          })
        }
        onAddNew={(name: string, domain: string, allDomains?: string[]) => {
          void handleAddNew(name, domain, allDomains);
        }}
        onAddToExisting={(
          setId: string,
          domain: string,
          allDomains?: string[],
        ) => {
          void handleAddToExisting(setId, domain, allDomains);
        }}
        loading={addingPreset}
      />
    </Stack>
  );
};
