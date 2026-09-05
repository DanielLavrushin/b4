import { useState, useRef, useCallback, useEffect, useMemo } from "react";
import { Box, Button, CircularProgress, Stack, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router";
import { StartIcon, DiscoveryIcon, HistoryIcon, ClearIcon } from "@b4.icons";
import { colors } from "@design";
import { B4SetConfig } from "@models/config";
import { DomainReassignment } from "@models/sets";
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
import { ApplyTarget, buildResultEntries } from "@utils";
import {
  DiscoveryOptionsPanel,
  DiscoveryOptions,
  loadOptions,
  saveOptions,
} from "./Options";
import { RunPanel } from "./RunPanel";
import { ResultsPanel } from "./ResultsPanel";
import { HistoryTable } from "./HistoryTable";
import { ApplyDialog } from "./ApplyDialog";
import { DiscoveryLogDialog, DiscoveryLogLine } from "./LogPanel";

const URL_SEPARATORS = /\s+|,(?=\s|$)/;

const extractDomain = (url: string): string => {
  try {
    const withProto = url.includes("://") ? url : `https://${url}`;
    return new URL(withProto).hostname;
  } catch {
    return url.split("/")[0];
  }
};

export const DiscoveryRunner = () => {
  const { t } = useTranslation();
  const navigate = useNavigate();
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
    resetDiscovery,
    addPresetAsSet,
    clearCache,
    clearHistory,
    deleteHistoryDomain,
  } = useDiscovery();
  const { logs, connected, clearLogs } = useDiscoveryLogs();
  const { addDomainsToSet } = useSets();
  const { captures, loadCaptures } = useCaptures();

  const [options, setOptions] = useState<DiscoveryOptions>(loadOptions);
  const [ipVersionEnabled, setIpVersionEnabled] = useState(true);
  const [checkUrls, setCheckUrls] = useState<string[]>([]);
  const [urlInput, setUrlInput] = useState("");
  const [logOpen, setLogOpen] = useState(false);
  const [applyTarget, setApplyTarget] = useState<ApplyTarget | null>(null);
  const [applying, setApplying] = useState(false);
  const domainInputRef = useRef<HTMLInputElement | null>(null);

  useEffect(() => {
    saveOptions(options);
  }, [options]);

  useEffect(() => {
    void loadCaptures();
  }, [loadCaptures]);

  useEffect(() => {
    void configApi
      .get()
      .then((c) => setIpVersionEnabled(!!c.queue?.ipv4 && !!c.queue?.ipv6))
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
    (urls: string[]) => {
      void startDiscovery(urls, {
        skipDNS: !options.checkDns,
        skipCache: !options.useCache,
        payloadFiles: options.payloadFiles,
        validationTries: options.validationTries,
        tlsVersion: options.tlsVersion,
        ipVersion: effectiveIpVersion,
      });
    },
    [startDiscovery, options, effectiveIpVersion],
  );

  const addUrls = useCallback((raw: string) => {
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

  const describeMoved = (moved?: DomainReassignment[]): string => {
    if (!moved || moved.length === 0) return "";
    return t("discovery.apply.moved", {
      domains: [...new Set(moved.map((m) => m.domain))].join(", "),
      sets: [...new Set(moved.map((m) => m.set_name))].join(", "),
    });
  };

  const handleCreate = async (set: B4SetConfig) => {
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

  const handleAddToExisting = async (setId: string, domains: string[]) => {
    setApplying(true);
    const res = await addDomainsToSet(setId, domains);
    setApplying(false);
    if (!res.success) {
      showError(t("discovery.apply.addFailed"));
      return;
    }
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

  const handleRerun = (url: string) => {
    setCheckUrls([url]);
    resetDiscovery();
    start([url]);
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
            canStop={suite.source !== "watchdog"}
            onStop={() => void cancelDiscovery()}
            logLine={logLine}
          />
        )}

        {showResults && suite && (
          <ResultsPanel
            suite={suite}
            entries={entries}
            applying={applying}
            canReset={!finishing}
            onApply={setApplyTarget}
            onShowLog={() => setLogOpen(true)}
            onNewSearch={resetDiscovery}
          />
        )}

        {!showRun && !showResults && !isReconnecting && (
          <>
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
                onClick={() => start(checkUrls)}
                disabled={checkUrls.length === 0 || busy}
                sx={{ whiteSpace: "nowrap" }}
              >
                {t("discovery.start")}
              </Button>
            </Box>
            <B4ChipList
              items={checkUrls}
              getKey={(url) => url}
              getLabel={(url) => extractDomain(url)}
              onDelete={removeUrl}
              emptyMessage={t("discovery.input.empty")}
              showEmpty
            />
            <DiscoveryOptionsPanel
              options={options}
              ipVersionEnabled={ipVersionEnabled}
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
            <Button
              size="small"
              startIcon={<ClearIcon />}
              onClick={handleClearHistory}
              sx={{ color: colors.text.secondary, textTransform: "none" }}
            >
              {t("discovery.history.clear")}
            </Button>
          }
        >
          <HistoryTable
            entries={history}
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
        onClose={() => setApplyTarget(null)}
        onCreate={(set) => void handleCreate(set)}
        onAddToExisting={(setId, domains) =>
          void handleAddToExisting(setId, domains)
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
