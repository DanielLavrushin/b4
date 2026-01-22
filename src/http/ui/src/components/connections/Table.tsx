import { ParsedLog } from "@b4.connections";
import { AddIcon } from "@b4.icons";
import { ProtocolChip } from "@common/ProtocolChip";
import { Badge } from "@primitives/badge";
import { Button } from "@primitives/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@primitives/table";
import { Tooltip, TooltipContent, TooltipTrigger } from "@primitives/tooltip";
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
  type ColumnDef,
  type SortingState,
} from "@tanstack/react-table";
import { asnStorage } from "@utils";
import { ArrowDown, ArrowUp, ArrowUpDown } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

export type SortColumn =
  | "timestamp"
  | "set"
  | "protocol"
  | "domain"
  | "source"
  | "destination";

export type SortDirection = "asc" | "desc" | null;

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

const SortableHeader = ({
  label,
  column,
  onSort,
}: {
  label: string;
  column: { getIsSorted: () => false | "asc" | "desc" };
  onSort: () => void;
}) => {
  const sortState = column.getIsSorted();
  return (
    <Button variant="ghost" onClick={onSort}>
      {label}
      {sortState === "asc" ? (
        <ArrowUp />
      ) : sortState === "desc" ? (
        <ArrowDown />
      ) : (
        <ArrowUpDown />
      )}
    </Button>
  );
};

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

  const sorting = useMemo<SortingState>(
    () =>
      sortColumn ? [{ id: sortColumn, desc: sortDirection === "desc" }] : [],
    [sortColumn, sortDirection],
  );

  const columns = useMemo<ColumnDef<ParsedLog>[]>(
    () => [
      {
        accessorKey: "timestamp",
        header: ({ column }) => (
          <SortableHeader
            label="Time"
            column={column}
            onSort={() => onSort("timestamp")}
          />
        ),
        cell: ({ row }) => row.original.timestamp.split(" ")[1],
      },
      {
        accessorKey: "protocol",
        header: ({ column }) => (
          <SortableHeader
            label="Protocol"
            column={column}
            onSort={() => onSort("protocol")}
          />
        ),
        cell: ({ row }) => <ProtocolChip protocol={row.original.protocol} />,
      },
      {
        accessorKey: "set",
        header: ({ column }) => (
          <SortableHeader
            label="Set"
            column={column}
            onSort={() => onSort("set")}
          />
        ),
        cell: ({ row }) => {
          const log = row.original;
          return log.ipSet || log.hostSet ? (
            <Badge variant="secondary">{log.ipSet || log.hostSet}</Badge>
          ) : null;
        },
      },
      {
        accessorKey: "domain",
        header: ({ column }) => (
          <SortableHeader
            label="Domain"
            column={column}
            onSort={() => onSort("domain")}
          />
        ),
        cell: ({ row }) => {
          const log = row.original;
          return (
            <div
              className="flex min-w-0 items-center gap-2"
              onClick={() =>
                log.domain && !log.hostSet && onDomainClick(log.domain)
              }
            >
              {log.domain && (
                <span className="min-w-0 flex-1 wrap-break-word">
                  {log.domain}
                </span>
              )}
              {log.domain && !log.hostSet && (
                <AddIcon
                  className="shrink-0"
                  onClick={(e) => {
                    e.stopPropagation();
                    onDomainClick(log.domain);
                  }}
                />
              )}
            </div>
          );
        },
      },
      {
        accessorKey: "source",
        header: ({ column }) => (
          <SortableHeader
            label="Source"
            column={column}
            onSort={() => onSort("source")}
          />
        ),
        cell: ({ row }) => {
          const log = row.original;
          return (
            <Tooltip>
              <TooltipTrigger
                render={
                  <span>
                    {log.deviceName ? (
                      <Badge>{log.deviceName}</Badge>
                    ) : (
                      <span>{log.source}</span>
                    )}
                  </span>
                }
              ></TooltipTrigger>
              <TooltipContent>
                <p>{log.source}</p>
              </TooltipContent>
            </Tooltip>
          );
        },
      },
      {
        accessorKey: "destination",
        header: ({ column }) => (
          <SortableHeader
            label="Destination"
            column={column}
            onSort={() => onSort("destination")}
          />
        ),
        cell: ({ row }) => {
          const log = row.original;
          const asnName = log.destination
            ? asnStorage.findAsnForIp(log.destination)?.name || null
            : null;

          return (
            <div
              className="flex min-w-0 items-center gap-2"
              onClick={() =>
                log.destination && !log.ipSet && onIpClick(log.destination)
              }
            >
              <span className="min-w-0 flex-1 wrap-break-word">
                {log.destination}
              </span>
              {!log.ipSet && (
                <AddIcon
                  className="shrink-0 align-sub"
                  onClick={(e) => {
                    e.stopPropagation();
                    onIpClick(log.destination);
                  }}
                />
              )}
              {asnName && (
                <Badge variant="outline" className="shrink-0">
                  {asnName}
                </Badge>
              )}
            </div>
          );
        },
      },
    ],
    [onSort, onDomainClick, onIpClick],
  );

  const table = useReactTable({
    data,
    columns,
    getCoreRowModel: getCoreRowModel(),
    manualSorting: true, // Сортировка управляется извне
    state: {
      sorting,
    },
    getRowId: (row) => row.raw,
  });

  const startIndex = Math.max(0, Math.floor(scrollTop / ROW_HEIGHT) - OVERSCAN);
  const visibleCount = Math.ceil(containerHeight / ROW_HEIGHT) + OVERSCAN * 2;
  const endIndex = Math.min(data.length, startIndex + visibleCount);

  const visibleRows = useMemo(
    () => table.getRowModel().rows.slice(startIndex, endIndex),
    [table.getRowModel().rows, startIndex, endIndex],
  );

  const handleScroll = useCallback(
    (e: React.UIEvent<HTMLDivElement>) => {
      const target = e.currentTarget;
      setScrollTop(target.scrollTop);

      const isAtBottom =
        target.scrollHeight - target.scrollTop - target.clientHeight < 50;
      onScrollStateChange(isAtBottom);
    },
    [onScrollStateChange],
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
      className="bg-background [&::-webkit-scrollbar-thumb]:bg-border hover:[&::-webkit-scrollbar-thumb]:bg-muted-foreground/50 absolute inset-0 overflow-auto [&::-webkit-scrollbar]:w-2 [&::-webkit-scrollbar-thumb]:rounded-full [&::-webkit-scrollbar-thumb]:border-2 [&::-webkit-scrollbar-thumb]:border-transparent [&::-webkit-scrollbar-thumb]:bg-clip-padding [&::-webkit-scrollbar-track]:bg-transparent"
    >
      <div className="overflow-hidden rounded-md border">
        <Table>
          <TableHeader className="bg-background sticky top-0 z-10">
            {table.getHeaderGroups().map((headerGroup) => (
              <TableRow key={headerGroup.id}>
                {headerGroup.headers.map((header) => {
                  return (
                    <TableHead key={header.id}>
                      {header.isPlaceholder
                        ? null
                        : flexRender(
                            header.column.columnDef.header,
                            header.getContext(),
                          )}
                    </TableHead>
                  );
                })}
              </TableRow>
            ))}
          </TableHeader>
          <TableBody>
            {table.getRowModel().rows?.length ? (
              <>
                {startIndex > 0 && (
                  <TableRow>
                    <TableCell
                      colSpan={columns.length}
                      style={{ height: startIndex * ROW_HEIGHT }}
                      className="border-0 p-0"
                    />
                  </TableRow>
                )}

                {visibleRows.map((row) => (
                  <TableRow
                    key={row.id}
                    data-state={row.getIsSelected() && "selected"}
                  >
                    {row.getVisibleCells().map((cell) => (
                      <TableCell key={cell.id}>
                        {flexRender(
                          cell.column.columnDef.cell,
                          cell.getContext(),
                        )}
                      </TableCell>
                    ))}
                  </TableRow>
                ))}

                {endIndex < table.getRowModel().rows.length && (
                  <TableRow>
                    <TableCell
                      colSpan={columns.length}
                      style={{
                        height:
                          (table.getRowModel().rows.length - endIndex) *
                          ROW_HEIGHT,
                      }}
                      className="border-0 p-0"
                    />
                  </TableRow>
                )}
              </>
            ) : (
              <TableRow>
                <TableCell
                  colSpan={columns.length}
                  className="h-24 text-center"
                >
                  Waiting for connections...
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>
    </div>
  );
};
