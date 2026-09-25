import { useEffect, useState } from "react";
import { Link as RouterLink, useNavigate } from "react-router";
import {
  Box,
  Button,
  CircularProgress,
  Link,
  Stack,
  Tooltip,
  Typography,
} from "@mui/material";
import { Trans, useTranslation } from "react-i18next";
import { AddIcon, DiscoveryIcon, SearchIcon } from "@b4.icons";
import {
  B4Alert,
  B4Badge,
  B4ChipList,
  B4Hint,
  B4PlusButton,
  B4Section,
  B4Switch,
  B4TextField,
} from "@b4.elements";
import { colors } from "@design";
import { B4SetConfig } from "@models/config";
import {
  ProbeSuggestion,
  SetRunRecord,
  SetVerdictStatus,
} from "@models/discovery";
import {
  setWatchBlock,
  setWatchTone,
  watchBlockClearsFlag,
} from "@models/watchdog";
import { discoveryApi } from "@api/discovery";
import { useWatchdogSetStatuses } from "@hooks/useWatchdog";
import {
  MAX_PROBE_URLS,
  formatTimeAgo,
  normalizeProbeUrl,
  presetLabel,
  probeUrlDisplay,
} from "@utils";

const VERDICT_COLOR: Record<
  SetVerdictStatus,
  "success" | "warning" | "error" | "default"
> = {
  covered: "success",
  current_works: "success",
  partial: "warning",
  none: "error",
  not_needed: "default",
  incomplete: "default",
};

interface DiscoveryTabProps {
  config: B4SetConfig;
  isNew: boolean;
  dirty: boolean;
  savedWatchdog: boolean;
  globalWatchdog: boolean;
  onChange: (field: string, value: string[] | boolean) => void;
}

export const DiscoveryTab = ({
  config,
  isNew,
  dirty,
  savedWatchdog,
  globalWatchdog,
  onChange,
}: DiscoveryTabProps) => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [input, setInput] = useState("");
  const [inputError, setInputError] = useState<string | null>(null);
  const [suggestions, setSuggestions] = useState<ProbeSuggestion[] | null>(
    null,
  );
  const [suggesting, setSuggesting] = useState(false);
  const [suggestFailed, setSuggestFailed] = useState(false);
  const [lastRun, setLastRun] = useState<SetRunRecord | null>(null);
  const [runsLoaded, setRunsLoaded] = useState(false);
  const [runsFailed, setRunsFailed] = useState(false);

  const urls = config.discovery?.urls ?? [];
  const routed = !!config.routing?.enabled;
  const full = urls.length >= MAX_PROBE_URLS;
  const hosts = new Set(
    urls.map((u) => normalizeProbeUrl(u)?.host).filter((h) => !!h),
  );

  const setUrls = (next: string[]) => onChange("discovery.urls", next);

  const watching = !!config.discovery?.watchdog;
  const watchBlock = setWatchBlock(config);
  const showWatchStatus = !isNew && savedWatchdog && globalWatchdog;
  const watchStatuses = useWatchdogSetStatuses(showWatchStatus);
  const watchEntry = showWatchStatus
    ? watchStatuses.byId.get(config.id)
    : undefined;

  useEffect(() => {
    if (isNew || routed || !config.id) return;
    let active = true;
    discoveryApi
      .setRuns(config.id)
      .then((list) => {
        if (!active) return;
        const own = (Array.isArray(list) ? list : []).filter(
          (r) => r.set_id === config.id,
        );
        setLastRun(own[0] ?? null);
        setRunsFailed(false);
        setRunsLoaded(true);
      })
      .catch(() => {
        if (!active) return;
        setLastRun(null);
        setRunsFailed(true);
        setRunsLoaded(true);
      });
    return () => {
      active = false;
    };
  }, [isNew, routed, config.id]);

  const add = () => {
    const raw = input.trim();
    if (!raw) return;
    const probe = normalizeProbeUrl(raw);
    if (!probe) {
      setInputError(t("sets.discovery.invalid"));
      return;
    }
    if (hosts.has(probe.host)) {
      setInputError(t("sets.discovery.duplicate", { host: probe.host }));
      return;
    }
    if (full) {
      setInputError(t("sets.discovery.full", { max: MAX_PROBE_URLS }));
      return;
    }
    setUrls([...urls, probe.url]);
    setInput("");
    setInputError(null);
  };

  const addSuggestion = (s: ProbeSuggestion) => {
    const probe = normalizeProbeUrl(s.url);
    if (!probe || hosts.has(probe.host) || full) return;
    setUrls([...urls, probe.url]);
  };

  const suggest = async () => {
    setSuggesting(true);
    setSuggestFailed(false);
    try {
      const res = await discoveryApi.suggest(config.id);
      setSuggestions(res?.urls ?? []);
    } catch {
      setSuggestions(null);
      setSuggestFailed(true);
    } finally {
      setSuggesting(false);
    }
  };

  const findStrategy = () => {
    navigate("/discovery", {
      state: { setId: config.id, urls },
    })?.catch(() => {});
  };

  const openDiscovery = () => {
    navigate("/discovery", {
      state: { setId: config.id },
    })?.catch(() => {});
  };

  const verdict = lastRun?.verdict;
  const runCovered = verdict?.covered ?? [];
  const runUncovered = verdict?.uncovered ?? [];
  const runDate = lastRun ? new Date(lastRun.end_time || lastRun.start_time) : null;

  const disabled = routed;

  return (
    <B4Section
      title={t("sets.discovery.sectionTitle")}
      description={t("sets.discovery.sectionDescription")}
      icon={<DiscoveryIcon />}
    >
      <Stack spacing={2}>
        {routed && (
          <B4Alert severity="info" noWrapper>
            {t("sets.discovery.routed")}
          </B4Alert>
        )}
        <B4Hint>{t("sets.discovery.explain")}</B4Hint>

        <Box sx={{ display: "flex", gap: 1, alignItems: "flex-start" }}>
          <B4TextField
            label={t("sets.discovery.inputLabel")}
            value={input}
            placeholder={t("sets.discovery.inputPlaceholder")}
            onChange={(e) => {
              setInput(e.target.value);
              setInputError(null);
            }}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                add();
              }
            }}
            error={!!inputError}
            helperText={
              inputError ??
              t("sets.discovery.inputHelper", { max: MAX_PROBE_URLS })
            }
            disabled={disabled || full}
          />
          <B4PlusButton
            onClick={add}
            disabled={disabled || full || !input.trim()}
          />
        </Box>

        {urls.length > 0 ? (
          <B4ChipList
            items={urls}
            getKey={(url) => url}
            getLabel={(url) => probeUrlDisplay(url)}
            onDelete={
              disabled
                ? undefined
                : (url) => setUrls(urls.filter((u) => u !== url))
            }
          />
        ) : (
          <Typography variant="body2" sx={{ color: colors.text.secondary }}>
            {t("sets.discovery.empty")}
          </Typography>
        )}

        <Stack direction="row" spacing={1} flexWrap="wrap" useFlexGap>
          <Button
            variant="outlined"
            startIcon={
              suggesting ? <CircularProgress size={16} /> : <SearchIcon />
            }
            onClick={() => void suggest()}
            disabled={disabled || isNew || suggesting}
          >
            {t("sets.discovery.suggest")}
          </Button>
          <Button
            variant="contained"
            startIcon={<DiscoveryIcon />}
            onClick={findStrategy}
            disabled={disabled || isNew}
          >
            {t("sets.discovery.findStrategy")}
          </Button>
        </Stack>
        {isNew && !routed && (
          <Typography variant="caption" sx={{ color: colors.text.secondary }}>
            {t("sets.discovery.saveFirst")}
          </Typography>
        )}
        {dirty && !isNew && !routed && (
          <Typography variant="caption" sx={{ color: colors.text.secondary }}>
            {t("sets.discovery.unsaved")}
          </Typography>
        )}

        <Box
          sx={{
            p: 1.5,
            border: `1px solid ${colors.border.default}`,
            borderRadius: 1,
          }}
        >
          <Stack spacing={1}>
            <B4Switch
              label={t("sets.discovery.watchdog.label")}
              description={t("sets.discovery.watchdog.explain")}
              checked={watching}
              disabled={!watching && !!watchBlock}
              onChange={(checked) => onChange("discovery.watchdog", checked)}
            />
            {watchBlock && (
              <Typography
                variant="caption"
                sx={{
                  color: watching ? colors.state.warning : colors.text.secondary,
                  display: "block",
                }}
              >
                {watching && watchBlockClearsFlag(watchBlock)
                  ? t("sets.discovery.watchdog.willTurnOff", {
                      reason: t(`watchdog.errors.${watchBlock}`),
                    })
                  : watching
                    ? t("sets.discovery.watchdog.paused", {
                        reason: t(`watchdog.errors.${watchBlock}`),
                      })
                    : t(`watchdog.errors.${watchBlock}`)}
              </Typography>
            )}
            {!globalWatchdog && (
              <Typography
                variant="caption"
                sx={{ color: colors.text.secondary, display: "block" }}
              >
                <Trans
                  i18nKey="sets.discovery.watchdog.globalOff"
                  components={{
                    a: <Link component={RouterLink} to="/settings/discovery" />,
                  }}
                />
              </Typography>
            )}
            {showWatchStatus && watchStatuses.loaded && (
              <Box
                sx={{
                  display: "flex",
                  alignItems: "center",
                  gap: 1,
                  flexWrap: "wrap",
                }}
              >
                {watchEntry ? (
                  <>
                    <B4Badge
                      variant="outlined"
                      color={setWatchTone(watchEntry.status)}
                      label={t(`watchdog.setStatus.${watchEntry.status}`, {
                        defaultValue: watchEntry.status,
                      })}
                    />
                    {watchEntry.reason && (
                      <Typography variant="body2">
                        {t(`watchdog.reason.${watchEntry.reason}`, {
                          defaultValue: watchEntry.reason,
                        })}
                      </Typography>
                    )}
                    {watchEntry.last_check && (
                      <Typography
                        variant="caption"
                        sx={{ color: colors.text.secondary }}
                      >
                        {t("sets.discovery.watchdog.lastCheck", {
                          time:
                            formatTimeAgo(t, watchEntry.last_check) || "-",
                        })}
                      </Typography>
                    )}
                    {watchEntry.last_heal_preset && (
                      <Typography
                        variant="caption"
                        sx={{ color: colors.text.secondary }}
                      >
                        {t("sets.discovery.watchdog.lastHeal", {
                          preset: presetLabel(watchEntry.last_heal_preset, t),
                          time:
                            formatTimeAgo(t, watchEntry.last_heal ?? "") || "-",
                        })}
                      </Typography>
                    )}
                    <Link
                      component={RouterLink}
                      to="/watchdog"
                      variant="caption"
                    >
                      {t("sets.discovery.watchdog.openWatchdog")}
                    </Link>
                  </>
                ) : (
                  <Typography
                    variant="caption"
                    sx={{ color: colors.text.secondary }}
                  >
                    {t("sets.discovery.watchdog.notTracked")}
                  </Typography>
                )}
              </Box>
            )}
          </Stack>
        </Box>

        {!isNew && !routed && runsLoaded && (
          <Box
            sx={{
              p: 1.5,
              border: `1px solid ${colors.border.default}`,
              borderRadius: 1,
              bgcolor: colors.background.dark,
            }}
          >
            <Typography
              variant="subtitle2"
              sx={{ color: colors.text.secondary, mb: 0.75 }}
            >
              {t("sets.discovery.lastRun")}
            </Typography>
            {runsFailed && (
              <Typography variant="body2" sx={{ color: colors.text.secondary }}>
                {t("sets.discovery.lastRunFailed")}
              </Typography>
            )}
            {!runsFailed && !lastRun && (
              <Typography variant="body2" sx={{ color: colors.text.secondary }}>
                {t("sets.discovery.lastRunNone")}
              </Typography>
            )}
            {verdict && (
              <Stack spacing={0.75}>
                <Box
                  sx={{
                    display: "flex",
                    alignItems: "center",
                    gap: 1,
                    flexWrap: "wrap",
                  }}
                >
                  <B4Badge
                    variant="outlined"
                    color={VERDICT_COLOR[verdict.status] ?? "default"}
                    label={t(`discovery.verdict.status.${verdict.status}`)}
                  />
                  {runDate && !Number.isNaN(runDate.getTime()) && (
                    <Tooltip title={runDate.toLocaleString()}>
                      <Typography
                        variant="caption"
                        sx={{ color: colors.text.secondary }}
                      >
                        {formatTimeAgo(
                          t,
                          lastRun?.end_time ?? "",
                          lastRun?.start_time,
                        )}
                      </Typography>
                    </Tooltip>
                  )}
                </Box>
                {verdict.winner_preset && (
                  <Typography variant="body2">
                    {t("sets.discovery.lastRunWinner", {
                      preset: presetLabel(verdict.winner_preset, t),
                    })}
                  </Typography>
                )}
                {runCovered.length > 0 && (
                  <Typography
                    variant="caption"
                    sx={{ color: colors.text.secondary, display: "block" }}
                  >
                    {t("sets.discovery.lastRunCovered", {
                      domains: runCovered.join(", "),
                    })}
                  </Typography>
                )}
                {runUncovered.length > 0 && (
                  <Typography
                    variant="caption"
                    sx={{ color: colors.text.secondary, display: "block" }}
                  >
                    {t("sets.discovery.lastRunUncovered", {
                      domains: runUncovered.join(", "),
                    })}
                  </Typography>
                )}
                <Box>
                  <Button
                    size="small"
                    startIcon={<DiscoveryIcon />}
                    onClick={openDiscovery}
                    sx={{ textTransform: "none" }}
                  >
                    {t("sets.discovery.openDiscovery")}
                  </Button>
                </Box>
              </Stack>
            )}
          </Box>
        )}

        {suggestFailed && (
          <B4Alert severity="error" noWrapper>
            {t("sets.discovery.suggestFailed")}
          </B4Alert>
        )}
        {suggestions && suggestions.length === 0 && (
          <B4Alert severity="info" noWrapper>
            {t("discovery.set.noSuggestions")}
          </B4Alert>
        )}
        {suggestions && suggestions.length > 0 && (
          <Stack spacing={1}>
            <Typography variant="subtitle2" sx={{ color: colors.text.secondary }}>
              {t("sets.discovery.suggestions")}
            </Typography>
            {suggestions.map((s) => {
              const present = hosts.has(s.host);
              const foreign =
                !!s.owner_set_id && s.owner_set_id !== config.id;
              return (
                <Box
                  key={s.url}
                  sx={{
                    display: "flex",
                    alignItems: "center",
                    gap: 1,
                    p: 1,
                    border: `1px solid ${colors.border.default}`,
                    borderRadius: 1,
                    bgcolor: colors.background.dark,
                  }}
                >
                  <Box sx={{ flex: 1, minWidth: 0 }}>
                    <Box
                      sx={{
                        display: "flex",
                        alignItems: "center",
                        gap: 1,
                        flexWrap: "wrap",
                      }}
                    >
                      <Typography
                        variant="body2"
                        sx={{ wordBreak: "break-all", fontWeight: 600 }}
                      >
                        {probeUrlDisplay(s.url)}
                      </Typography>
                      <B4Badge
                        size="small"
                        variant="outlined"
                        label={t(`discovery.set.source.${s.source}`)}
                      />
                    </Box>
                    {foreign && (
                      <Typography
                        variant="caption"
                        sx={{ color: colors.state.warning, display: "block" }}
                      >
                        {t("sets.discovery.foreignOwner", {
                          owner: s.owner_set_name || s.owner_set_id,
                        })}
                      </Typography>
                    )}
                  </Box>
                  <Button
                    size="small"
                    startIcon={<AddIcon />}
                    onClick={() => addSuggestion(s)}
                    disabled={disabled || present || full}
                    sx={{ flexShrink: 0 }}
                  >
                    {present
                      ? t("sets.discovery.added")
                      : t("sets.discovery.add")}
                  </Button>
                </Box>
              );
            })}
          </Stack>
        )}
      </Stack>
    </B4Section>
  );
};
