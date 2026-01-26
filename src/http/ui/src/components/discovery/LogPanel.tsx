import { useDiscoveryLogs } from "@b4.discovery";
import { ClearIcon, LogsIcon } from "@b4.icons";
import { cn } from "@design/lib/utils";
import {
  Card,
  CardAction,
  CardContent,
  CardHeader,
  CardTitle,
} from "@design/primitives/card";
import { Item } from "@design/primitives/item";
import { ScrollArea } from "@design/primitives/scroll-area";
import { Badge } from "@primitives/badge";
import { Button } from "@primitives/button";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@primitives/collapsible";
import { Tooltip, TooltipContent, TooltipTrigger } from "@primitives/tooltip";
import { useEffect, useRef, useState } from "react";

interface DiscoveryLogPanelProps {
  running: boolean;
}

export const DiscoveryLogPanel = ({ running }: DiscoveryLogPanelProps) => {
  const { logs, clearLogs } = useDiscoveryLogs();
  const [expanded, setExpanded] = useState(false);
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

  if (!running && logs.length === 0) return null;

  return (
    <Collapsible open={expanded} onOpenChange={setExpanded}>
      {/* Header */}
      <Card>
        <CollapsibleTrigger>
          <CardHeader>
            <CardTitle className="flex flex-row items-center gap-2">
              <LogsIcon />
              Discovery Logs
              {logs.length > 0 && (
                <Badge className="ml-auto">{`${logs.length} lines`}</Badge>
              )}
            </CardTitle>

            {logs.length > 0 && (
              <CardAction>
                <Tooltip>
                  <TooltipTrigger>
                    <Button
                      size="sm"
                      variant="ghost"
                      onClick={(e) => {
                        e.stopPropagation();
                        clearLogs();
                      }}
                    >
                      <ClearIcon />
                    </Button>
                  </TooltipTrigger>
                  <TooltipContent>
                    <p>Clear logs</p>
                  </TooltipContent>
                </Tooltip>
              </CardAction>
            )}
          </CardHeader>
        </CollapsibleTrigger>

        {/* Log content */}
        <CollapsibleContent>
          <CardContent className="p-0">
            <ScrollArea className="bg-background h-37.5 wrap-break-word whitespace-pre-wrap">
              {logs.length === 0 ? (
                <Item className="text-muted-foreground">
                  Waiting for discovery logs...
                </Item>
              ) : (
                logs.map((line, i) => (
                  <p
                    key={i}
                    className={cn("hover:bg-accent/50", getLogColorClass(line))}
                  >
                    {line}
                  </p>
                ))
              )}
            </ScrollArea>
          </CardContent>
        </CollapsibleContent>
      </Card>
    </Collapsible>
  );
};

function getLogColorClass(line: string): string {
  const lower = line.toLowerCase();
  if (lower.includes("success") || lower.includes("best"))
    return "text-primary";
  if (lower.includes("failed") || line.includes("✗") || lower.includes("fail"))
    return "text-destructive";
  if (lower.includes("phase")) return "text-muted-foreground";
  return "text-foreground";
}
