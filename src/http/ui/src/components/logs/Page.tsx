import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Box, Container, Paper, Stack } from "@mui/material";
import { ClearIcon } from "@b4.icons";
import { B4Badge, B4TextField, B4Switch, B4TooltipButton } from "@b4.elements";
import { colors } from "@design";
import { useWebSocket } from "@context/B4WsProvider";
import { useSnackbar } from "@context/SnackbarProvider";
import { useTranslation } from "react-i18next";
import i18n from "@/i18n";
import { useTraceSession } from "@hooks/useTraceSession";
import {
  LogLevel,
  LOG_LEVELS,
  loadEnabledLevels,
  parseLogLine,
  saveEnabledLevels,
} from "./parse";
import { LevelFilterBar } from "./LevelFilterBar";
import { LogViewport } from "./LogViewport";
import { TraceControls } from "./TraceControls";

export function LogsPage() {
  const { t } = useTranslation();
  const { showSuccess } = useSnackbar();
  const [filter, setFilter] = useState("");
  const [enabledLevels, setEnabledLevels] =
    useState<Set<LogLevel>>(loadEnabledLevels);
  const [autoScroll, setAutoScroll] = useState(true);
  const [showScrollBtn, setShowScrollBtn] = useState(false);
  const logRef = useRef<HTMLDivElement | null>(null);
  const { logs, pauseLogs, setPauseLogs, clearLogs } = useWebSocket();
  const trace = useTraceSession();

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
          borderColor: trace.tracing
            ? colors.state.error
            : pauseLogs
              ? colors.border.strong
              : colors.border.default,
          transition: "border-color 0.3s",
        }}
      >
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

            <TraceControls
              tracing={trace.tracing}
              traceBusy={trace.traceBusy}
              traceLines={trace.traceLines}
              traceElapsed={trace.traceElapsed}
              downloadReady={trace.downloadReady}
              onStart={trace.startTrace}
              onStop={trace.stopTrace}
              onDownload={trace.downloadTrace}
            />

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

          <LevelFilterBar
            enabledLevels={enabledLevels}
            levelCounts={levelCounts}
            onToggle={toggleLevel}
          />
        </Box>

        <LogViewport
          totalCount={logs.length}
          filtered={filtered}
          showScrollBtn={showScrollBtn}
          scrollRef={logRef}
          onScroll={handleScroll}
          onScrollToBottom={scrollToBottom}
        />
      </Paper>
    </Container>
  );
}
