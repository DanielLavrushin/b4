import { ArrowDownIcon, ClearIcon } from "@b4.icons";
import { useWebSocket } from "@context/B4WsProvider";
import { useSnackbar } from "@context/SnackbarProvider";
import { Badge } from "@design/components/ui/badge";
import { Button } from "@design/components/ui/button";
import { Input } from "@design/components/ui/input";
import { Label } from "@design/components/ui/label";
import { Switch } from "@design/components/ui/switch";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@design/components/ui/tooltip";
import { cn } from "@design/lib/utils";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

export function LogsPage() {
  const { showSuccess } = useSnackbar();
  const [filter, setFilter] = useState("");
  const [autoScroll, setAutoScroll] = useState(true);
  const [showScrollBtn, setShowScrollBtn] = useState(false);
  const logRef = useRef<HTMLDivElement | null>(null);
  const { logs, pauseLogs, setPauseLogs, clearLogs } = useWebSocket();

  useEffect(() => {
    const el = logRef.current;
    if (el && autoScroll) {
      el.scrollTop = el.scrollHeight;
    }
  }, [logs, autoScroll]);

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

  const filtered = useMemo(() => {
    const f = filter.trim().toLowerCase();
    return f ? logs.filter((l) => l.toLowerCase().includes(f)) : logs;
  }, [logs, filter]);

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
        showSuccess("Logs cleared");
      } else if (e.key === "p" || e.key === "Pause") {
        e.preventDefault();
        setPauseLogs(!pauseLogs);
        showSuccess(`Logs ${!pauseLogs ? "paused" : "resumed"}`);
      }
    },
    [clearLogs, pauseLogs, setPauseLogs, showSuccess]
  );

  useEffect(() => {
    globalThis.window.addEventListener("keydown", handleHotkeysDown);
    return () => {
      globalThis.window.removeEventListener("keydown", handleHotkeysDown);
    };
  }, [handleHotkeysDown]);

  return (
    <div className="flex-1 flex flex-col overflow-hidden">
      <div
        className={cn(
          "flex-1 flex flex-col overflow-hidden border transition-colors",
          pauseLogs ? "border-border/50" : "border-border"
        )}
      >
        {/* Controls Bar */}
        <div className="p-4 border-b border-border/50 bg-card">
          <div className="flex flex-row gap-4 items-center">
            <Input
              placeholder="Filter logs..."
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              className="flex-1"
            />
            <div className="flex flex-row gap-2 items-center">
              <Badge>
                {`${logs.length} lines`}
              </Badge>
              {filter && (
                <Badge>
                  {`${filtered.length} filtered`}
                </Badge>
              )}
            </div>
            <div className="flex items-center gap-2">
              <Switch
                checked={pauseLogs}
                onCheckedChange={(checked: boolean) => setPauseLogs(checked)}
              />
              <Label className="font-medium cursor-pointer">
                {pauseLogs ? "Paused" : "Streaming"}
              </Label>
            </div>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button variant="ghost" size="icon-sm" onClick={clearLogs}>
                  <ClearIcon />
                </Button>
              </TooltipTrigger>
              <TooltipContent>
                <p>Clear Logs</p>
              </TooltipContent>
            </Tooltip>
          </div>
        </div>

        <div
          ref={logRef}
          onScroll={handleScroll}
          className="flex-1 overflow-y-auto relative p-4 font-mono text-[13px] leading-relaxed whitespace-pre-wrap wrap-break-word bg-background text-foreground"
        >
          {(() => {
            if (filtered.length === 0 && logs.length === 0) {
              return (
                <p className="text-muted-foreground text-center mt-8 italic">
                  Waiting for logs...
                </p>
              );
            } else if (filtered.length === 0) {
              return (
                <p className="text-muted-foreground text-center mt-8 italic">
                  No logs match your filter
                </p>
              );
            } else {
              return filtered.map((l, i) => (
                <div
                  key={l + "_" + i}
                  className="font-mono text-[13px] hover:bg-accent/50"
                >
                  {l}
                </div>
              ));
            }
          })()}

          {/* Scroll to Bottom Button */}
          {showScrollBtn && (
            <Button
              onClick={scrollToBottom}
              size="icon"
              className="absolute bottom-4 right-4 bg-primary text-primary-foreground shadow-lg hover:bg-primary/80"
            >
              <ArrowDownIcon className="h-4 w-4" />
            </Button>
          )}
        </div>
      </div>
    </div>
  );
}
