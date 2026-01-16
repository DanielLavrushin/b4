import { ArrowDownIcon, ClearIcon } from "@b4.icons";
import { useWebSocket } from "@context/B4WsProvider";
import { useSnackbar } from "@context/SnackbarProvider";
import { Badge } from "@primitives/badge";
import { Button } from "@primitives/button";
import { Input } from "@primitives/input";
import { Label } from "@primitives/label";
import { Switch } from "@primitives/switch";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@primitives/tooltip";
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
    [clearLogs, pauseLogs, setPauseLogs, showSuccess],
  );

  useEffect(() => {
    globalThis.window.addEventListener("keydown", handleHotkeysDown);
    return () => {
      globalThis.window.removeEventListener("keydown", handleHotkeysDown);
    };
  }, [handleHotkeysDown]);

  return (
    <div className="flex flex-1 flex-col overflow-hidden">
      <div
        className={cn(
          "flex flex-1 flex-col overflow-hidden border transition-colors",
          pauseLogs ? "border-border/50" : "border-border",
        )}
      >
        {/* Controls Bar */}
        <div className="border-border/50 bg-card border-b p-4">
          <div className="flex flex-row items-center gap-4">
            <Input
              placeholder="Filter logs..."
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              className="flex-1"
            />
            <div className="flex flex-row items-center gap-2">
              <Badge>{`${logs.length} lines`}</Badge>
              {filter && <Badge>{`${filtered.length} filtered`}</Badge>}
            </div>
            <div className="flex items-center gap-2">
              <Switch
                checked={pauseLogs}
                onCheckedChange={(checked: boolean) => setPauseLogs(checked)}
              />
              <Label className="cursor-pointer font-medium">
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
          className="bg-background text-foreground relative flex-1 overflow-y-auto p-4 font-mono text-[13px] leading-relaxed wrap-break-word whitespace-pre-wrap"
        >
          {(() => {
            if (filtered.length === 0 && logs.length === 0) {
              return (
                <p className="text-muted-foreground mt-8 text-center italic">
                  Waiting for logs...
                </p>
              );
            } else if (filtered.length === 0) {
              return (
                <p className="text-muted-foreground mt-8 text-center italic">
                  No logs match your filter
                </p>
              );
            } else {
              return filtered.map((l, i) => (
                <div
                  key={l + "_" + i}
                  className="hover:bg-accent/50 font-mono text-[13px]"
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
              className="bg-primary text-primary-foreground hover:bg-primary/80 absolute right-4 bottom-4 shadow-lg"
            >
              <ArrowDownIcon />
            </Button>
          )}
        </div>
      </div>
    </div>
  );
}
