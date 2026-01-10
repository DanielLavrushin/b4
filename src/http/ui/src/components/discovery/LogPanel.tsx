import { useDiscoveryLogs } from "@b4.discovery";
import { ClearIcon, CollapseIcon, ExpandIcon, LogsIcon } from "@b4.icons";
import { Badge } from "@design/components/ui/badge";
import { Button } from "@design/components/ui/button";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@design/components/ui/collapsible";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@design/components/ui/tooltip";
import { cn } from "@design/lib/utils";
import { useEffect, useRef, useState } from "react";

interface DiscoveryLogPanelProps {
  running: boolean;
}

export const DiscoveryLogPanel = ({ running }: DiscoveryLogPanelProps) => {
  const { logs, connected, clearLogs } = useDiscoveryLogs();
  const [expanded, setExpanded] = useState(false);
  const scrollRef = useRef<HTMLDivElement>(null);
  const hasAutoExpanded = useRef(false);

  useEffect(() => {
    if (running && logs.length > 0 && !hasAutoExpanded.current) {
      setExpanded(true);
      hasAutoExpanded.current = true;
    }
    if (!running) {
      hasAutoExpanded.current = false;
    }
  }, [running, logs.length]);

  useEffect(() => {
    if (scrollRef.current && expanded) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [logs, expanded]);

  if (!running && logs.length === 0) return null;

  return (
    <div className="flex flex-col overflow-hidden border transition-colors border-border">
      <Collapsible open={expanded} onOpenChange={setExpanded}>
        {/* Header */}
        <CollapsibleTrigger asChild>
          <div className="p-4 border-b border-border/50 bg-card flex items-center justify-between cursor-pointer hover:bg-accent/50 transition-colors">
            <div className="flex items-center gap-3">
              <LogsIcon className="h-5 w-5 text-secondary" />
              <h6 className="text-base font-semibold text-foreground">
                Discovery Logs
              </h6>
              <div
                className={cn(
                  "w-4 h-4 rounded-full",
                  connected ? "bg-secondary" : "bg-muted-foreground"
                )}
              />
              {logs.length > 0 && (
                <Badge>{`${logs.length} lines`}</Badge>
              )}
            </div>
            <div className="flex items-center gap-1">
              {logs.length > 0 && (
                <Tooltip>
                  <TooltipTrigger asChild>
                    <Button
                      size="sm"
                      variant="ghost"
                      onClick={(e) => {
                        e.stopPropagation();
                        clearLogs();
                      }}
                      className="h-8 w-8 p-0"
                    >
                      <ClearIcon className="h-4 w-4" />
                    </Button>
                  </TooltipTrigger>
                  <TooltipContent>
                    <p>Clear logs</p>
                  </TooltipContent>
                </Tooltip>
              )}
              <Button
                size="sm"
                variant="ghost"
                onClick={(e) => {
                  e.stopPropagation();
                  setExpanded((prev) => !prev);
                }}
                className="h-8 w-8 p-0"
              >
                {expanded ? (
                  <CollapseIcon className="h-4 w-4" />
                ) : (
                  <ExpandIcon className="h-4 w-4" />
                )}
              </Button>
            </div>
          </div>
        </CollapsibleTrigger>

        {/* Log content */}
        <CollapsibleContent>
          <div
            ref={scrollRef}
            className="h-37.5 overflow-y-auto relative p-4 font-mono text-[13px] leading-relaxed whitespace-pre-wrap wrap-break-word bg-background text-foreground"
          >
            {logs.length === 0 ? (
              <p className="text-muted-foreground italic">
                Waiting for discovery logs...
              </p>
            ) : (
              logs.map((line, i) => (
                <div
                  key={i}
                  className={cn(
                    "font-mono text-[13px] hover:bg-accent/50",
                    getLogColorClass(line)
                  )}
                >
                  {line}
                </div>
              ))
            )}
          </div>
        </CollapsibleContent>
      </Collapsible>
    </div>
  );
};

function getLogColorClass(line: string): string {
  const lower = line.toLowerCase();
  if (lower.includes("success") || line.includes("✓") || lower.includes("best"))
    return "text-secondary-foreground";
  if (lower.includes("failed") || line.includes("✗") || lower.includes("fail"))
    return "text-destructive";
  if (lower.includes("phase")) return "text-muted-foreground";
  return "text-foreground";
}
