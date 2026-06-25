import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  Box,
  Button,
  Chip,
  Container,
  IconButton,
  Paper,
  Stack,
  Typography,
} from "@mui/material";
import {
  ClearIcon,
  ArrowDownIcon,
  FilterIcon,
  StartIcon,
  StopIcon,
  DownloadIcon,
} from "@b4.icons";
import { B4Badge, B4TextField, B4Switch, B4TooltipButton } from "@b4.elements";
import { colors, fonts, glows } from "@design";
import { useWebSocket } from "@context/B4WsProvider";
import { useSnackbar } from "@context/SnackbarProvider";
import { useTranslation } from "react-i18next";
import i18n from "@/i18n";
import { traceApi } from "@api/trace";
import {
  LogLevel,
  LOG_LEVELS,
  loadEnabledLevels,
  parseLogLine,
  ParsedLogLine,
  saveEnabledLevels,
} from "./parse";

const levelColor: Record<LogLevel, string> = {
  error: colors.state.error,
  warn: colors.state.warning,
  info: colors.state.info,
  trace: colors.text.secondary,
  debug: colors.text.secondary,
};

const rowTheme: Record<
  LogLevel,
  { border: string; tint: string; text: string }
> = {
  error: {
    border: colors.state.error,
    tint: "rgba(244, 67, 54, 0.10)",
    text: colors.text.primary,
  },
  warn: {
    border: colors.state.warning,
    tint: "rgba(255, 167, 38, 0.08)",
    text: colors.text.primary,
  },
  info: {
    border: "rgba(245, 173, 24, 0.20)",
    tint: "transparent",
    text: colors.text.primary,
  },
  trace: {
    border: "rgba(255, 255, 255, 0.08)",
    tint: "transparent",
    text: colors.text.disabled,
  },
  debug: {
    border: "rgba(255, 255, 255, 0.08)",
    tint: "transparent",
    text: colors.text.disabled,
  },
};

const unparsedTheme = {
  border: "rgba(245, 173, 24, 0.20)",
  tint: "transparent",
  text: colors.text.primary,
};

function trimTime(time: string): string {
  return time.replace(/(\.\d{3})\d*$/, "$1");
}

function formatElapsed(seconds: number): string {
  const m = Math.floor(seconds / 60)
    .toString()
    .padStart(2, "0");
  const s = (seconds % 60).toString().padStart(2, "0");
  return `${m}:${s}`;
}

function LogRow({ line }: { line: ParsedLogLine }) {
  const theme = line.level ? rowTheme[line.level] : unparsedTheme;
  return (
    <Box
      sx={{
        display: "flex",
        gap: 1.5,
        pl: 1.25,
        borderLeft: `2px solid ${theme.border}`,
        bgcolor: theme.tint,
        color: theme.text,
        "&:hover": { bgcolor: colors.accent.primaryStrong },
      }}
    >
      {line.time && (
        <Box
          component="span"
          title={`${line.date ?? ""} ${line.time}`.trim()}
          sx={{
            flexShrink: 0,
            color: colors.text.disabled,
            userSelect: "none",
          }}
        >
          {trimTime(line.time)}
        </Box>
      )}
      <Box component="span" sx={{ flex: 1, minWidth: 0 }}>
        {line.message}
      </Box>
    </Box>
  );
}

export function LogsPage() {
  const { t } = useTranslation();
  const { showSuccess, showError } = useSnackbar();
  const [filter, setFilter] = useState("");
  const [tracing, setTracing] = useState(false);
  const [traceBusy, setTraceBusy] = useState(false);
  const [traceLines, setTraceLines] = useState(0);
  const [traceElapsed, setTraceElapsed] = useState(0);
  const [traceStartMs, setTraceStartMs] = useState<number | null>(null);
  const [downloadReady, setDownloadReady] = useState(false);
  const [enabledLevels, setEnabledLevels] =
    useState<Set<LogLevel>>(loadEnabledLevels);
  const [autoScroll, setAutoScroll] = useState(true);
  const [showScrollBtn, setShowScrollBtn] = useState(false);
  const logRef = useRef<HTMLDivElement | null>(null);
  const { logs, pauseLogs, setPauseLogs, clearLogs } = useWebSocket();

  const parsed = useMemo(() => logs.map(parseLogLine), [logs]);

  const levelCounts = useMemo(() => {
    const counts: Record<LogLevel, number> = {
      error: 0,
      warn: 0,
      info: 0,
      trace: 0,
      debug: 0,
    };
    for (const line of parsed) {
      if (line.level) counts[line.level]++;
    }
    return counts;
  }, [parsed]);

  const filtered = useMemo(() => {
    const f = filter.trim().toLowerCase();
    return parsed.filter((line) => {
      if (line.level && !enabledLevels.has(line.level)) return false;
      if (f && !line.raw.toLowerCase().includes(f)) return false;
      return true;
    });
  }, [parsed, filter, enabledLevels]);

  useEffect(() => {
    const el = logRef.current;
    if (el && autoScroll) {
      el.scrollTop = el.scrollHeight;
    }
  }, [filtered, autoScroll]);

  const handleScroll = () => {
    const el = logRef.current;
    if (el) {
      const isAtBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 50;
      setAutoScroll(isAtBottom);
      setShowScrollBtn(!isAtBottom);
    }
  };

  const scrollToBottom = () => {
    const el = logRef.current;
    if (el) {
      el.scrollTop = el.scrollHeight;
      setAutoScroll(true);
      setShowScrollBtn(false);
    }
  };

  useEffect(() => {
    saveEnabledLevels(enabledLevels);
  }, [enabledLevels]);

  useEffect(() => {
    let cancelled = false;
    traceApi
      .status()
      .then((s) => {
        if (cancelled) return;
        setDownloadReady(s.downloadReady);
        if (s.active) {
          setTracing(true);
          setTraceLines(s.lines);
          setTraceStartMs(
            s.startedAt ? new Date(s.startedAt).getTime() : Date.now(),
          );
        }
      })
      .catch(() => undefined);
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    if (!tracing || traceStartMs == null) return;
    const tick = () =>
      setTraceElapsed(Math.floor((Date.now() - traceStartMs) / 1000));
    tick();
    const elapsedTimer = setInterval(tick, 1000);
    const pollTimer = setInterval(() => {
      traceApi
        .status()
        .then((s) => {
          setTraceLines(s.lines);
          if (!s.active) {
            setTracing(false);
            setTraceStartMs(null);
            setDownloadReady(s.downloadReady);
          }
        })
        .catch(() => undefined);
    }, 2000);
    return () => {
      clearInterval(elapsedTimer);
      clearInterval(pollTimer);
    };
  }, [tracing, traceStartMs]);

  const startTrace = async () => {
    setTraceBusy(true);
    try {
      const s = await traceApi.start();
      setTracing(true);
      setTraceLines(s.lines);
      setTraceElapsed(0);
      setTraceStartMs(
        s.startedAt ? new Date(s.startedAt).getTime() : Date.now(),
      );
      showSuccess(t("logs.trace.started"));
    } catch {
      showError(t("logs.trace.startFailed"));
    } finally {
      setTraceBusy(false);
    }
  };

  const stopTrace = async () => {
    setTraceBusy(true);
    try {
      const s = await traceApi.stop();
      setTracing(false);
      setTraceStartMs(null);
      setTraceLines(s.lines);
      setDownloadReady(true);
      await traceApi.download();
      showSuccess(t("logs.trace.saved"));
    } catch {
      showError(t("logs.trace.stopFailed"));
    } finally {
      setTraceBusy(false);
    }
  };

  const downloadTrace = () => {
    traceApi.download().catch(() => showError(t("logs.trace.stopFailed")));
  };

  const toggleLevel = (level: LogLevel) => {
    setEnabledLevels((prev) => {
      const next = new Set(prev);
      if (next.has(level)) {
        next.delete(level);
      } else {
        next.add(level);
      }
      return next;
    });
  };

  const handleHotkeysDown = useCallback(
    (e: KeyboardEvent) => {
      const target = e.target as HTMLElement;
      if (
        target.tagName === "INPUT" ||
        target.tagName === "TEXTAREA" ||
        target.isContentEditable
      ) {
        return;
      }

      if ((e.ctrlKey && e.key === "x") || e.key === "Delete") {
        e.preventDefault();
        clearLogs();
        showSuccess(i18n.t("logs.cleared"));
      } else if (e.key === "p" || e.key === "Pause") {
        e.preventDefault();
        setPauseLogs(!pauseLogs);
        showSuccess(pauseLogs ? i18n.t("logs.resumed") : i18n.t("logs.paused"));
      }
    },
    [clearLogs, pauseLogs, setPauseLogs, showSuccess],
  );

  useEffect(() => {
    globalThis.window.addEventListener("keydown", handleHotkeysDown);
    return () => {
      globalThis.window.removeEventListener("keydown", handleHotkeysDown);
    };
  }, [handleHotkeysDown]);

  return (
    <Container
      maxWidth={false}
      sx={{
        flex: 1,
        py: 3,
        px: 3,
        display: "flex",
        flexDirection: "column",
        overflow: "hidden",
      }}
    >
      <Paper
        elevation={0}
        variant="outlined"
        sx={{
          flex: 1,
          display: "flex",
          flexDirection: "column",
          overflow: "hidden",
          border: "1px solid",
          borderColor: tracing
            ? colors.state.error
            : pauseLogs
              ? colors.border.strong
              : colors.border.default,
          transition: "border-color 0.3s",
        }}
      >
        {/* Controls Bar */}
        <Box
          sx={{
            p: 2,
            borderBottom: `1px solid ${colors.border.light}`,
            bgcolor: colors.background.control,
          }}
        >
          <Stack direction="row" spacing={2} alignItems="center">
            <B4TextField
              size="small"
              placeholder={t("logs.filterPlaceholder")}
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
            />
            <Stack direction="row" spacing={1} alignItems="center">
              <B4Badge
                label={t("core.lines", { count: logs.length })}
                size="small"
              />
              {(filter || enabledLevels.size < LOG_LEVELS.length) && (
                <B4Badge
                  label={t("core.filtered", { count: filtered.length })}
                  size="small"
                />
              )}
            </Stack>

            <Box sx={{ flexGrow: 1 }} />

            {tracing ? (
              <Stack direction="row" spacing={1.5} alignItems="center">
                <Stack
                  direction="row"
                  spacing={1}
                  alignItems="center"
                  sx={{
                    px: 1.25,
                    py: 0.5,
                    borderRadius: 1,
                    border: `1px solid ${colors.state.error}`,
                    bgcolor: "rgba(244, 67, 54, 0.10)",
                    fontFamily: fonts.mono,
                    fontSize: 12,
                    color: colors.text.primary,
                    whiteSpace: "nowrap",
                  }}
                >
                  <Box
                    sx={{
                      width: 9,
                      height: 9,
                      borderRadius: "50%",
                      bgcolor: colors.state.error,
                      "@keyframes b4recpulse": {
                        "0%, 100%": { opacity: 1 },
                        "50%": { opacity: 0.25 },
                      },
                      animation: "b4recpulse 1.2s ease-in-out infinite",
                    }}
                  />
                  <span>
                    {t("logs.trace.recording")} {formatElapsed(traceElapsed)}
                  </span>
                  <span style={{ color: colors.text.disabled }}>
                    · {t("core.lines", { count: traceLines })}
                  </span>
                </Stack>
                <Button
                  size="small"
                  variant="contained"
                  color="error"
                  disabled={traceBusy}
                  startIcon={<StopIcon />}
                  onClick={() => void stopTrace()}
                  sx={{ flexShrink: 0, whiteSpace: "nowrap" }}
                >
                  {t("logs.trace.stop")}
                </Button>
              </Stack>
            ) : (
              <Stack direction="row" spacing={0.5} alignItems="center">
                <Button
                  size="small"
                  variant="contained"
                  disabled={traceBusy}
                  startIcon={<StartIcon />}
                  onClick={() => void startTrace()}
                  title={t("logs.trace.startHint")}
                  sx={{ flexShrink: 0, whiteSpace: "nowrap" }}
                >
                  {t("logs.trace.start")}
                </Button>
                {downloadReady && (
                  <B4TooltipButton
                    title={t("logs.trace.downloadLast")}
                    onClick={downloadTrace}
                    icon={<DownloadIcon />}
                  />
                )}
              </Stack>
            )}

            <B4Switch
              label={
                pauseLogs ? t("logs.pausedLabel") : t("logs.streamingLabel")
              }
              checked={pauseLogs}
              onChange={(checked: boolean) => setPauseLogs(checked)}
            />
            <B4TooltipButton
              title={t("logs.clearLogs")}
              onClick={clearLogs}
              icon={<ClearIcon />}
            />
          </Stack>

          <Stack
            direction="row"
            spacing={1}
            alignItems="center"
            sx={{ mt: 1.5, flexWrap: "wrap", rowGap: 1 }}
          >
            <FilterIcon
              sx={{ fontSize: 16, color: colors.text.disabled, mr: 0.5 }}
            />
            {LOG_LEVELS.map((level) => {
              const active = enabledLevels.has(level);
              const color = levelColor[level];
              return (
                <Chip
                  key={level}
                  size="small"
                  label={`${level.toUpperCase()} ${levelCounts[level]}`}
                  onClick={() => toggleLevel(level)}
                  sx={{
                    fontFamily: fonts.mono,
                    fontSize: 11,
                    letterSpacing: "0.04em",
                    cursor: "pointer",
                    color: active ? color : colors.text.disabled,
                    borderColor: active ? color : colors.border.light,
                    bgcolor: active
                      ? colors.background.hover
                      : "transparent",
                    border: "1px solid",
                    opacity: active ? 1 : 0.6,
                    "&:hover": { bgcolor: colors.background.hover },
                  }}
                  variant="outlined"
                />
              );
            })}
          </Stack>
        </Box>

        <Box
          ref={logRef}
          onScroll={handleScroll}
          sx={{
            flex: 1,
            overflowY: "auto",
            position: "relative",
            p: 2,
            fontFamily: fonts.mono,
            fontSize: 12.5,
            lineHeight: 1.7,
            whiteSpace: "pre-wrap",
            wordBreak: "break-word",
            backgroundColor: colors.background.dark,
            color: colors.text.primary,
          }}
        >
          {(() => {
            if (filtered.length === 0 && logs.length === 0) {
              return (
                <Typography
                  sx={{
                    color: colors.text.secondary,
                    textAlign: "center",
                    mt: 4,
                    fontStyle: "italic",
                  }}
                >
                  {t("logs.waitingForLogs")}
                </Typography>
              );
            } else if (filtered.length === 0) {
              return (
                <Typography
                  sx={{
                    color: colors.text.secondary,
                    textAlign: "center",
                    mt: 4,
                    fontStyle: "italic",
                  }}
                >
                  {t("logs.noMatch")}
                </Typography>
              );
            } else {
              return filtered.map((line, i) => (
                <LogRow key={line.raw + "_" + i} line={line} />
              ));
            }
          })()}

          {/* Scroll to Bottom Button */}
          {showScrollBtn && (
            <IconButton
              onClick={scrollToBottom}
              sx={{
                position: "absolute",
                bottom: 16,
                right: 16,
                bgcolor: colors.primary,
                color: colors.text.primary,
                boxShadow: glows.primary,
                "&:hover": { bgcolor: colors.tertiary },
              }}
              size="small"
            >
              <ArrowDownIcon />
            </IconButton>
          )}
        </Box>
      </Paper>
    </Container>
  );
}
