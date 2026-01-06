import { ParsedLog } from "@b4.connections";
import { AddIcon } from "@b4.icons";
import { ProtocolChip } from "@common/ProtocolChip";
import { SortableTableCell, SortDirection } from "@common/SortableTableCell";
import { Badge } from "@design/components/ui/badge";
import {
  Table,
  TableBody,
  TableCell,
  TableHeader,
  TableRow,
} from "@design/components/ui/table";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@design/components/ui/tooltip";
import { asnStorage } from "@utils";
import { memo, useCallback, useEffect, useMemo, useRef, useState } from "react";

export type SortColumn =
  | "timestamp"
  | "set"
  | "protocol"
  | "domain"
  | "source"
  | "destination";

interface DomainsTableProps {
  data: ParsedLog[];
  sortColumn: SortColumn | null;
  sortDirection: SortDirection;
  onSort: (column: SortColumn) => void;
  onDomainClick: (domain: string) => void;
  onIpClick: (ip: string) => void;
  onScrollStateChange: (isAtBottom: boolean) => void;
}

const ROW_HEIGHT = 41;
const OVERSCAN = 5;

const TableRowMemo = memo<{
  log: ParsedLog;
  onDomainClick: (domain: string) => void;
  onIpClick: (ip: string) => void;
}>(
  ({ log, onDomainClick, onIpClick }) => {
    const asnName = useMemo(() => {
      if (!log.destination) return null;
      const asn = asnStorage.findAsnForIp(log.destination);
      return asn?.name || null;
    }, [log.destination]);

    return (
      <TableRow>
        <TableCell variant="mono">{log.timestamp.split(" ")[1]}</TableCell>
        <TableCell>
          <ProtocolChip protocol={log.protocol} />
        </TableCell>
        <TableCell>
          {(log.ipSet || log.hostSet) && (
            <Badge variant="secondary">{log.ipSet || log.hostSet}</Badge>
          )}
        </TableCell>
        <TableCell
          onClick={() =>
            log.domain && !log.hostSet && onDomainClick(log.domain)
          }
        >
          <div>
            {log.domain && <span>{log.domain}</span>}
            <div className="flex-1" />
            {log.domain && !log.hostSet && (
              <AddIcon
                onClick={(e) => {
                  e.stopPropagation();
                  onDomainClick(log.domain!);
                }}
              />
            )}
          </div>
        </TableCell>
        <TableCell variant="mono">
          <Tooltip>
            <TooltipTrigger asChild>
              <span>
                {log.deviceName ? (
                  <Badge>{log.deviceName}</Badge>
                ) : (
                  <span>{log.source}</span>
                )}
              </span>
            </TooltipTrigger>
            <TooltipContent>
              <p>{log.source}</p>
            </TooltipContent>
          </Tooltip>
        </TableCell>
        <TableCell>
          <div>
            <span
              onClick={() =>
                log.destination && !log.ipSet && onIpClick(log.destination)
              }
            >
              {log.destination}
            </span>
            {asnName && <Badge variant="outline">{asnName}</Badge>}
            <div className="flex-1" />
            {!log.ipSet && (
              <AddIcon
                onClick={(e) => {
                  e.stopPropagation();
                  onIpClick(log.destination!);
                }}
              />
            )}
          </div>
        </TableCell>
      </TableRow>
    );
  },
  (prev, next) => prev.log.raw === next.log.raw
);

TableRowMemo.displayName = "TableRowMemo";

export const DomainsTable = ({
  data,
  sortColumn,
  sortDirection,
  onSort,
  onDomainClick,
  onIpClick,
  onScrollStateChange,
}: DomainsTableProps) => {
  const containerRef = useRef<HTMLDivElement>(null);
  const [scrollTop, setScrollTop] = useState(0);
  const [containerHeight, setContainerHeight] = useState(600);

  const startIndex = Math.max(0, Math.floor(scrollTop / ROW_HEIGHT) - OVERSCAN);
  const visibleCount = Math.ceil(containerHeight / ROW_HEIGHT) + OVERSCAN * 2;
  const endIndex = Math.min(data.length, startIndex + visibleCount);

  const visibleData = useMemo(
    () => data.slice(startIndex, endIndex),
    [data, startIndex, endIndex]
  );

  const handleScroll = useCallback(
    (e: React.UIEvent<HTMLDivElement>) => {
      const target = e.currentTarget;
      setScrollTop(target.scrollTop);

      const isAtBottom =
        target.scrollHeight - target.scrollTop - target.clientHeight < 50;
      onScrollStateChange(isAtBottom);
    },
    [onScrollStateChange]
  );

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    const observer = new ResizeObserver((entries) => {
      for (const entry of entries) {
        setContainerHeight(entry.contentRect.height);
      }
    });

    observer.observe(container);
    setContainerHeight(container.clientHeight);

    return () => observer.disconnect();
  }, []);

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    requestAnimationFrame(() => {
      const isAtBottom =
        container.scrollHeight - container.scrollTop - container.clientHeight <
        100;
      if (isAtBottom) {
        container.scrollTop = container.scrollHeight;
        setScrollTop(container.scrollTop);
      }
    });
  }, [data.length]);

  return (
    <div
      ref={containerRef}
      onScroll={handleScroll}
      className="flex-1 bg-background overflow-auto"
    >
      <Table className="[&>div]:overflow-visible">
        <TableHeader className="sticky top-0 z-1">
          <TableRow>
            <SortableTableCell
              label="Time"
              active={sortColumn === "timestamp"}
              direction={sortColumn === "timestamp" ? sortDirection : null}
              onSort={() => onSort("timestamp")}
            />
            <SortableTableCell
              label="Protocol"
              active={sortColumn === "protocol"}
              direction={sortColumn === "protocol" ? sortDirection : null}
              onSort={() => onSort("protocol")}
            />
            <SortableTableCell
              label="Set"
              active={sortColumn === "set"}
              direction={sortColumn === "set" ? sortDirection : null}
              onSort={() => onSort("set")}
            />
            <SortableTableCell
              label="Domain"
              active={sortColumn === "domain"}
              direction={sortColumn === "domain" ? sortDirection : null}
              onSort={() => onSort("domain")}
            />
            <SortableTableCell
              label="Source"
              active={sortColumn === "source"}
              direction={sortColumn === "source" ? sortDirection : null}
              onSort={() => onSort("source")}
            />
            <SortableTableCell
              label="Destination"
              active={sortColumn === "destination"}
              direction={sortColumn === "destination" ? sortDirection : null}
              onSort={() => onSort("destination")}
            />
          </TableRow>
        </TableHeader>
        <TableBody>
          {data.length === 0 ? (
            <TableRow>
              <TableCell
                colSpan={6}
                className="text-center py-8 text-muted-foreground italic"
              >
                Waiting for connections...
              </TableCell>
            </TableRow>
          ) : (
            <>
              {startIndex > 0 && (
                <TableRow>
                  <TableCell
                    colSpan={6}
                    style={{ height: startIndex * ROW_HEIGHT }}
                    className="p-0 border-0"
                  />
                </TableRow>
              )}

              {visibleData.map((log) => (
                <TableRowMemo
                  key={log.raw}
                  log={log}
                  onDomainClick={onDomainClick}
                  onIpClick={onIpClick}
                />
              ))}

              {endIndex < data.length && (
                <TableRow>
                  <TableCell
                    colSpan={6}
                    style={{ height: (data.length - endIndex) * ROW_HEIGHT }}
                    className="p-0 border-0"
                  />
                </TableRow>
              )}
            </>
          )}
        </TableBody>
      </Table>
    </div>
  );
};
