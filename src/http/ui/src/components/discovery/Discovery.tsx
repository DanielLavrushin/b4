import { useState, useRef, useCallback, useEffect, useMemo } from "react";
import { Box, Button, CircularProgress, Stack, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";
import { useLocation, useNavigate } from "react-router";
import {
  StartIcon,
  DiscoveryIcon,
  HistoryIcon,
  ClearIcon,
  LogsIcon,
} from "@b4.icons";
import { colors } from "@design";
import { B4SetConfig } from "@models/config";
import { ProbeSuggestion } from "@models/discovery";
import { DomainReassignment, SetDomainMatch } from "@models/sets";
import {
  B4Alert,
  B4Section,
  B4TextField,
  B4ChipList,
  B4PlusButton,
} from "@b4.elements";
import { useSnackbar } from "@context/SnackbarProvider";
import { useDiscovery, useDiscoveryLogs } from "@hooks/useDiscovery";
import { useSets } from "@hooks/useSets";
import { useCaptures } from "@b4.capture";
import { configApi } from "@b4.settings";
import { discoveryApi } from "@api/discovery";
import { setsApi } from "@api/sets";
import {
  ApplyTarget,
  MAX_PROBE_URLS,
  ProbeUrl,
  buildResultEntries,
  describeApiError,
  normalizeProbeUrl,
  probeUrlLabel,
  sanitizeProbeUrls,
} from "@utils";
import {
  DiscoveryOptionsPanel,
  DiscoveryOptions,
  loadOptions,
  saveOptions,
} from "./Options";
import { RunPanel } from "./RunPanel";
import { ResultsPanel } from "./ResultsPanel";
import { HistoryTable } from "./HistoryTable";
import { ApplyDialog, ReplaceOptions } from "./ApplyDialog";
import { DiscoveryLogDialog, DiscoveryLogLine } from "./LogPanel";
import { SetPicker, SetUrlHints } from "./SetPicker";
import { VerdictApply } from "./SetVerdictCard";

const URL_SEPARATORS = /\s+|,(?=\s|$)/;

interface DiscoveryLocationState {
  urls?: string[];
  setId?: string;
}

export const DiscoveryRunner = () => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const location = useLocation();
  const { showSuccess, showError } = useSnackbar();
  const {
    running,
    finishing,
    stopping,
    suiteId,
    suite,
    error,
    history,
    startDiscovery,
    cancelDiscovery,
    finishDiscovery,
    finishRequested,
    resetDiscovery,
    addPresetAsSet,
    replaceStrategy,
    markApplied,
    clearCache,
    clearHistory,
    deleteHistoryDomain,
  } = useDiscovery();
  const { logs, connected, clearLogs } = useDiscoveryLogs();
  const { addDomainsToSet } = useSets();
  const { captures, loadCaptures } = useCaptures();

  const [options, setOptions] = useState<DiscoveryOptions>(loadOptions);
  const [ipVersionEnabled, setIpVersionEnabled] = useState(true);
  const [communityEnabled, setCommunityEnabled] = useState(false);
  const [checkUrls, setCheckUrls] = useState<string[]>([]);
  const [urlInput, setUrlInput] = useState("");
  const [logOpen, setLogOpen] = useState(false);
  const [applyTarget, setApplyTarget] = useState<ApplyTarget | null>(null);
  const [applying, setApplying] = useState(false);
  const [refused, setRefused] = useState<string[]>([]);
  const [sets, setSets] = useState<B4SetConfig[]>([]);
  const [setsLoaded, setSetsLoaded] = useState(false);
  const [pickedSetId, setPickedSetId] = useState<string | null>(null);
  const [suggestions, setSuggestions] = useState<ProbeSuggestion[]>([]);
  const [suggestionsLoaded, setSuggestionsLoaded] = useState(false);
  const [owners, setOwners] = useState<SetDomainMatch[]>([]);
  const domainInputRef = useRef<HTMLInputElement | null>(null);
  const suggestFor = useRef<string | null>(null);
  const restoredSuite = useRef<string | null>(null);

  const loadSets = useCallback(async () => {
    try {
      const list = await setsApi.getSets();
      setSets(Array.isArray(list) ? list : []);
    } catch {
      setSets([]);
    } finally {
      setSetsLoaded(true);
    }
  }, []);

  useEffect(() => {
    void loadSets();
  }, [loadSets]);

  const pickedSet = useMemo(
    () =>
      pickedSetId
        ? sets.find((s) => s.id === pickedSetId && !s.routing?.enabled)
        : undefined,
    [sets, pickedSetId],
  );

  useEffect(() => {
    saveOptions(options);
  }, [options]);

  useEffect(() => {
    void loadCaptures();
  }, [loadCaptures]);

  useEffect(() => {
    void configApi
      .get()
      .then((c) => {
        setIpVersionEnabled(!!c.queue?.ipv4 && !!c.queue?.ipv6);
        setCommunityEnabled(Boolean(c.system?.hub?.enabled));
      })
      .catch(() => {});
  }, []);

  const effectiveIpVersion = ipVersionEnabled ? options.ipVersion : "auto";
  const isReconnecting = !!suiteId && running && !suite;
  const showRun = running && !!suite;
  const showResults = !!suite && !running;
  const busy = running || finishing || isReconnecting;

  const entries = useMemo(
    () => (suite && !running ? buildResultEntries(suite, true) : []),
    [suite, running],
  );

  const start = useCallback(
    (urls: string[], setId?: string | null) => {
      void startDiscovery(urls, {
        skipDNS: !options.checkDns,
        skipCache: !options.useCache,
        skipCommunity: !communityEnabled || !options.useCommunity,
        payloadFiles: options.payloadFiles,
        validationTries: options.validationTries,
        tlsVersion: options.tlsVersion,
        ipVersion: effectiveIpVersion,
        setId: setId ?? undefined,
        stopWhenCovered: setId ? options.stopWhenCovered : undefined,
      });
    },
    [startDiscovery, options, effectiveIpVersion, communityEnabled],
  );

  const appendUrls = useCallback((probes: ProbeUrl[]) => {
    setCheckUrls((prev) => {
      const hosts = new Set(prev.map((u) => normalizeProbeUrl(u)?.host ?? u));
      const next = [...prev];
      for (const probe of probes) {
        if (!hosts.has(probe.host)) {
          hosts.add(probe.host);
          next.push(probe.url);
        }
      }
      return next;
    });
  }, []);

  const addUrls = useCallback(
    (raw: string) => {
      const parts = raw
        .split(URL_SEPARATORS)
        .map((l) =>
          l
            .trim()
            .replace(/^["'`]+|["'`]+$/g, "")
            .trim(),
        )
        .filter((l) => l.length > 0);
      if (parts.length === 0) return;
      const bad: string[] = [];
      const good: ProbeUrl[] = [];
      for (const part of parts) {
        const probe = normalizeProbeUrl(part);
        if (probe) good.push(probe);
        else bad.push(part);
      }
      setRefused(bad);
      appendUrls(good);
      setUrlInput("");
    },
    [appendUrls],
  );

  const addSuggested = useCallback(
    (url: string) => {
      const probe = normalizeProbeUrl(url);
      if (probe) appendUrls([probe]);
    },
    [appendUrls],
  );

  const loadSuggestions = useCallback((setId: string, fill: boolean) => {
    suggestFor.current = setId;
    setSuggestions([]);
    setSuggestionsLoaded(false);
    discoveryApi
      .suggest(setId)
      .then((res) => {
        if (suggestFor.current !== setId) return;
        const list = res?.urls ?? [];
        setSuggestions(list);
        setSuggestionsLoaded(true);
        if (fill) {
          const first = sanitizeProbeUrls(list.map((s) => s.url));
          setCheckUrls((prev) => (prev.length === 0 ? first : prev));
        }
      })
      .catch(() => {
        if (suggestFor.current !== setId) return;
        setSuggestions([]);
        setSuggestionsLoaded(true);
      });
  }, []);

  const pickSet = useCallback(
    (setId: string | null, urls?: string[]) => {
      setRefused([]);
      setPickedSetId(setId);
      if (!setId) {
        suggestFor.current = null;
        setSuggestions([]);
        setSuggestionsLoaded(false);
        return;
      }
      const set = sets.find((s) => s.id === setId);
      const given = sanitizeProbeUrls(urls ?? []);
      const stored = sanitizeProbeUrls(set?.discovery?.urls ?? []);
      const initial = given.length > 0 ? given : stored;
      setCheckUrls(initial);
      loadSuggestions(setId, initial.length === 0);
    },
    [sets, loadSuggestions],
  );

  const hostsKey = useMemo(
    () =>
      checkUrls
        .map((u) => normalizeProbeUrl(u)?.host)
        .filter((h): h is string => !!h)
        .join(","),
    [checkUrls],
  );

  useEffect(() => {
    setOwners([]);
    if (!pickedSetId || !hostsKey) return;
    let active = true;
    setsApi
      .checkDomain(hostsKey)
      .then((found) => {
        if (active) setOwners(Array.isArray(found) ? found : []);
      })
      .catch(() => {
        if (active) setOwners([]);
      });
    return () => {
      active = false;
    };
  }, [pickedSetId, hostsKey]);

  useEffect(() => {
    if (!suite?.set_id || restoredSuite.current === suite.id) return;
    restoredSuite.current = suite.id;
    if (suite.set_id === pickedSetId) return;
    setPickedSetId(suite.set_id);
    const urls = sanitizeProbeUrls(
      (suite.domains ?? []).map((d) => d.check_url),
    );
    setCheckUrls(urls);
    loadSuggestions(suite.set_id, false);
  }, [suite, pickedSetId, loadSuggestions]);

  const handleUrlKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLInputElement>) => {
      if (e.key === "Enter" || e.key === "Tab") {
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
      if (URL_SEPARATORS.test(text)) {
        e.preventDefault();
        addUrls(text);
      }
    },
    [addUrls],
  );

  const removeUrl = useCallback((url: string) => {
    setCheckUrls((prev) => prev.filter((u) => u !== url));
  }, []);

  useEffect(() => {
    const state = location.state as DiscoveryLocationState | null;
    if (!state?.setId && !state?.urls?.length) return;
    if (state.setId && !setsLoaded) return;
    const target = state.setId
      ? sets.find((s) => s.id === state.setId && !s.routing?.enabled)
      : undefined;
    if (target) pickSet(target.id, state.urls);
    else if (state.urls?.length) addUrls(state.urls.join("\n"));
    navigate(
      { pathname: location.pathname, search: location.search, hash: location.hash },
      { replace: true, state: null },
    )?.catch(() => {});
  }, [
    location.pathname,
    location.search,
    location.hash,
    location.state,
    sets,
    setsLoaded,
    addUrls,
    pickSet,
    navigate,
  ]);

  const describeMoved = (moved?: DomainReassignment[]): string => {
    if (!moved || moved.length === 0) return "";
    return t("discovery.apply.moved", {
      domains: [...new Set(moved.map((m) => m.domain))].join(", "),
      sets: [...new Set(moved.map((m) => m.set_name))].join(", "),
    });
  };

  const handleCreate = async (set: B4SetConfig) => {
    const applied = applyTarget;
    setApplying(true);
    const res = await addPresetAsSet(set);
    setApplying(false);
    if (!res.success) {
      showError(
        [t("discovery.apply.createFailed"), res.error]
          .filter(Boolean)
          .join(" "),
      );
      return;
    }
    const id = res.data?.id;
    if (applied) await markApplied(applied.domains, applied.preset, id);
    void loadSets();
    showSuccess(
      [
        t("discovery.apply.created", { name: res.data?.name ?? set.name }),
        describeMoved(res.data?.moved),
      ]
        .filter(Boolean)
        .join(" "),
      id
        ? {
            label: t("discovery.apply.openSet"),
            onClick: () => {
              void navigate(`/sets/${id}`);
            },
          }
        : undefined,
    );
    setApplyTarget(null);
  };

  const handleAddToExisting = async (
    setId: string,
    domains: string[],
    pins?: Record<string, string[]>,
  ) => {
    const applied = applyTarget;
    setApplying(true);
    const res = await addDomainsToSet(setId, domains, pins);
    setApplying(false);
    if (!res.success) {
      showError(t("discovery.apply.addFailed"));
      return;
    }
    if (applied) await markApplied(applied.domains, applied.preset, setId);
    void loadSets();
    showSuccess(
      [t("discovery.apply.added"), describeMoved(res.data?.moved)]
        .filter(Boolean)
        .join(" "),
      {
        label: t("discovery.apply.openSet"),
        onClick: () => {
          void navigate(`/sets/${setId}`);
        },
      },
    );
    setApplyTarget(null);
  };

  const handleReplaceStrategy = async (
    setId: string,
    set: B4SetConfig,
    domains: string[],
    pins: Record<string, string[]> | undefined,
    opts: ReplaceOptions,
  ) => {
    const applied = applyTarget;
    setApplying(true);
    const res = await replaceStrategy(setId, set, domains, pins, {
      strategyOnly: true,
      keepTargets: opts.keepTargets,
      probeUrls: opts.probeUrls,
    });
    setApplying(false);
    if (!res.success) {
      showError(
        [t("discovery.apply.replaceFailed"), res.error]
          .filter(Boolean)
          .join(" "),
      );
      return;
    }
    if (applied) await markApplied(applied.domains, applied.preset, setId);
    void loadSets();
    showSuccess(
      [
        t("discovery.apply.replaced", { name: res.data?.name ?? "" }),
        describeMoved(res.data?.moved),
      ]
        .filter(Boolean)
        .join(" "),
      {
        label: t("discovery.apply.openSet"),
        onClick: () => {
          void navigate(`/sets/${setId}`);
        },
      },
    );
    setApplyTarget(null);
  };

  const handleApplyVerdict = async (req: VerdictApply): Promise<boolean> => {
    setApplying(true);
    const res = await replaceStrategy(req.setId, req.set, req.domains, req.pins, {
      strategyOnly: true,
      keepTargets: true,
      probeUrls: req.probeUrls,
    });
    setApplying(false);
    if (!res.success) {
      showError(
        [t("discovery.apply.replaceFailed"), res.error]
          .filter(Boolean)
          .join(" "),
      );
      return false;
    }
    await markApplied(req.domains, req.preset, req.setId);
    void loadSets();
    const name =
      res.data?.name ?? sets.find((s) => s.id === req.setId)?.name ?? "";
    showSuccess(
      [t("discovery.apply.replaced", { name }), describeMoved(res.data?.moved)]
        .filter(Boolean)
        .join(" "),
      {
        label: t("discovery.apply.openSet"),
        onClick: () => {
          void navigate(`/sets/${req.setId}`);
        },
      },
    );
    return true;
  };

  const handleSaveSetUrls = async (
    setId: string,
    urls: string[],
    removed: string[],
    dropHosts: string[],
  ): Promise<void> => {
    let target: B4SetConfig | undefined;
    try {
      const fresh = await setsApi.getSets();
      target = (Array.isArray(fresh) ? fresh : []).find((s) => s.id === setId);
    } catch {
      target = sets.find((s) => s.id === setId);
    }
    if (!target) return;
    const drop = new Set(dropHosts);
    const stored = target.discovery?.urls ?? [];
    const kept =
      stored.length > 0
        ? stored.filter((u) => {
            const host = normalizeProbeUrl(u)?.host;
            return !host || !drop.has(host);
          })
        : urls;
    try {
      await setsApi.updateSet(target.id, {
        ...target,
        discovery: { ...target.discovery, urls: kept },
      });
      showSuccess(
        t("discovery.verdict.pruned", {
          domains: removed.join(", "),
          name: target.name,
        }),
      );
      await loadSets();
    } catch (e) {
      showError(
        [t("discovery.verdict.pruneFailed"), describeApiError(e)]
          .filter(Boolean)
          .join(" "),
      );
    }
  };

  const handleRerun = (url: string, setId?: string) => {
    const known = setId
      ? sets.find((s) => s.id === setId && !s.routing?.enabled)
      : undefined;
    const stored = sanitizeProbeUrls(known?.discovery?.urls ?? []);
    const runSet = known && stored.length > 0 ? known.id : null;
    const urls = runSet ? stored : [url];
    setRefused([]);
    setPickedSetId(runSet);
    if (!runSet) {
      suggestFor.current = null;
      setSuggestions([]);
      setSuggestionsLoaded(false);
    }
    setCheckUrls(urls);
    resetDiscovery();
    start(urls, runSet);
  };

  const handleRemoveHistory = (domain: string) => {
    void (async () => {
      const res = await deleteHistoryDomain(domain);
      if (res.success) {
        showSuccess(t("discovery.history.removed", { domain }));
      }
    })();
  };

  const handleClearHistory = () => {
    void (async () => {
      const res = await clearHistory();
      if (res.success) showSuccess(t("discovery.history.cleared"));
      else showError(t("discovery.history.clearFailed"));
    })();
  };

  const handleClearCache = () => {
    void (async () => {
      const res = await clearCache();
      if (res.success) showSuccess(t("discovery.options.cacheCleared"));
      else showError(t("discovery.options.cacheClearFailed"));
    })();
  };

  const runSet = suite?.set_id
    ? sets.find((s) => s.id === suite.set_id)
    : undefined;
  const runSetName = runSet?.name;
  const setTooMany = !!pickedSet && checkUrls.length > MAX_PROBE_URLS;

  const logLine = (
    <DiscoveryLogLine
      logs={logs}
      connected={connected}
      onOpen={() => setLogOpen(true)}
    />
  );

  return (
    <Stack spacing={3}>
      <B4Section
        title={t("discovery.title")}
        description={t("discovery.description")}
        icon={<DiscoveryIcon />}
      >
        {isReconnecting && (
          <Box sx={{ display: "flex", alignItems: "center", gap: 2 }}>
            <CircularProgress size={20} sx={{ color: colors.secondary }} />
            <Typography variant="body2" sx={{ color: colors.text.secondary }}>
              {t("discovery.reconnecting")}
            </Typography>
          </Box>
        )}

        {showRun && suite && (
          <RunPanel
            suite={suite}
            stopping={stopping}
            finishRequested={finishRequested}
            canStop={suite.source !== "watchdog"}
            onStop={() => void cancelDiscovery()}
            onFinish={() => void finishDiscovery()}
            logLine={logLine}
            setName={runSetName}
          />
        )}

        {showResults && suite && (
          <ResultsPanel
            suite={suite}
            entries={entries}
            history={history}
            applying={applying}
            canReset={!finishing}
            setName={runSetName}
            runSet={runSet}
            onApply={setApplyTarget}
            onApplyVerdict={handleApplyVerdict}
            onSaveSetUrls={handleSaveSetUrls}
            onShowLog={() => setLogOpen(true)}
            onNewSearch={resetDiscovery}
          />
        )}

        {!showRun && !showResults && !isReconnecting && (
          <>
            {sets.length > 0 && (
              <SetPicker
                sets={sets}
                value={pickedSet ? pickedSet.id : null}
                disabled={busy}
                onChange={(id) => pickSet(id)}
              />
            )}
            <Box
              sx={{
                display: "flex",
                gap: 1,
                alignItems: "flex-start",
                flexWrap: "wrap",
                "& > .MuiFormControl-root": { flex: "1 1 220px", minWidth: 0 },
              }}
            >
              <B4TextField
                label={t("discovery.input.label")}
                value={urlInput}
                onChange={(e) => setUrlInput(e.target.value)}
                onKeyDown={handleUrlKeyDown}
                onPaste={handleUrlPaste}
                inputRef={domainInputRef}
                placeholder={t("discovery.input.placeholder")}
                disabled={busy}
                helperText={t("discovery.input.helper")}
              />
              <B4PlusButton
                onClick={() => addUrls(urlInput)}
                disabled={!urlInput.trim() || busy}
              />
              <Button
                startIcon={<StartIcon />}
                variant="contained"
                onClick={() => start(checkUrls, pickedSet?.id)}
                disabled={checkUrls.length === 0 || busy || setTooMany}
                sx={{ whiteSpace: "nowrap" }}
              >
                {t("discovery.start")}
              </Button>
            </Box>
            {refused.length > 0 && (
              <B4Alert severity="warning" onClose={() => setRefused([])}>
                {t("discovery.input.refused", { items: refused.join(", ") })}
              </B4Alert>
            )}
            <B4ChipList
              items={checkUrls}
              getKey={(url) => url}
              getLabel={(url) => probeUrlLabel(url)}
              onDelete={removeUrl}
              emptyMessage={t("discovery.input.empty")}
              showEmpty
            />
            {pickedSet && (
              <SetUrlHints
                set={pickedSet}
                urls={checkUrls}
                suggestions={suggestions}
                suggestionsLoaded={suggestionsLoaded}
                owners={owners}
                disabled={busy}
                onAdd={addSuggested}
              />
            )}
            <DiscoveryOptionsPanel
              options={options}
              ipVersionEnabled={ipVersionEnabled}
              communityEnabled={communityEnabled}
              setPicked={!!pickedSet}
              onChange={setOptions}
              onClearCache={handleClearCache}
              captures={captures}
              disabled={busy}
            />
          </>
        )}

        {finishing && !running && (
          <B4Alert severity="info">{t("discovery.finishing")}</B4Alert>
        )}
        {error && <B4Alert severity="error">{error}</B4Alert>}
      </B4Section>

      {history.length > 0 && (
        <B4Section
          title={t("discovery.history.title")}
          description={`${t("discovery.history.sites", { count: history.length })} · ${t("discovery.history.newestFirst")}`}
          icon={<HistoryIcon />}
          action={
            <Stack direction="row" spacing={1}>
              <Button
                size="small"
                startIcon={<LogsIcon />}
                onClick={() => setLogOpen(true)}
                sx={{ color: colors.text.secondary, textTransform: "none" }}
              >
                {t("discovery.history.lastLog")}
              </Button>
              <Button
                size="small"
                startIcon={<ClearIcon />}
                onClick={handleClearHistory}
                sx={{ color: colors.text.secondary, textTransform: "none" }}
              >
                {t("discovery.history.clear")}
              </Button>
            </Stack>
          }
        >
          <HistoryTable
            entries={history}
            sets={sets}
            busy={busy || applying}
            onApply={setApplyTarget}
            onRerun={handleRerun}
            onRemove={handleRemoveHistory}
          />
        </B4Section>
      )}

      <ApplyDialog
        open={applyTarget !== null}
        target={applyTarget}
        loading={applying}
        runSetId={applyTarget?.setId ?? null}
        sets={sets}
        onClose={() => setApplyTarget(null)}
        onCreate={(set) => void handleCreate(set)}
        onAddToExisting={(setId, domains, pins) =>
          void handleAddToExisting(setId, domains, pins)
        }
        onReplaceStrategy={(setId, set, domains, pins, opts) =>
          void handleReplaceStrategy(setId, set, domains, pins, opts)
        }
      />

      <DiscoveryLogDialog
        open={logOpen}
        logs={logs}
        onClose={() => setLogOpen(false)}
        onClear={clearLogs}
      />
    </Stack>
  );
};
