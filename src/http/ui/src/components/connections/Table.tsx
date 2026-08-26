import { useRef, useState, useEffect, useCallback, useMemo, memo } from "react";
import {
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Typography,
  Stack,
  Box,
  Tooltip,
  CircularProgress,
} from "@mui/material";
import { AddIcon, NetworkIcon } from "@b4.icons";
import { SortableTableCell, SortDirection } from "@common/SortableTableCell";
import { ProtocolChip } from "@common/ProtocolChip";
import { colors } from "@design";
import { B4Badge } from "@common/B4Badge";
import { asnStorage, stripPort } from "@utils";
import { ParsedLog } from "@b4.connections";
import { useTranslation } from "react-i18next";

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
  onEnrichIp: (ip: string) => Promise<void>;
  onDeleteAsn: (asnId: string) => void;
  enrichingIps: Set<string>;
  asnVersion: number;
  onScrollStateChange: (isAtBottom: boolean) => void;
}

const ROW_HEIGHT = 41;
const OVERSCAN = 5;

const TableRowMemo = memo<{
  log: ParsedLog;
  onDomainClick: (domain: string) => void;
  onIpClick: (ip: string) => void;
  onEnrichIp: (ip: string) => Promise<void>;
  onDeleteAsn: (asnId: string) => void;
  enrichingIps: Set<string>;
  asnVersion: number;
}>(
  ({
    log,
    onDomainClick,
    onIpClick,
    onEnrichIp,
    onDeleteAsn,
    enrichingIps,
  }) => {
    const { t } = useTranslation();
    const asnInfo = useMemo(() => {
      if (!log.destination) return null;
      return asnStorage.findAsnForIp(log.destination);
    }, [log.destination]);

    const isEnriching = enrichingIps.has(stripPort(log.destination));

    return (
      <TableRow
        sx={{
          height: ROW_HEIGHT,
          "&:hover": {
            bgcolor: colors.accent.primaryStrong,
          },
        }}
      >
        <TableCell
          sx={{
            color: "text.secondary",
            fontFamily: "monospace",
            fontSize: 12,
            borderBottom: `1px solid ${colors.border.light}`,
            py: 1,
          }}
        >
          {log.timestamp.split(" ")[1]}
        </TableCell>
        <TableCell
          sx={{
            borderBottom: `1px solid ${colors.border.light}`,
            py: 1,
          }}
        >
          <ProtocolChip protocol={log.protocol} flags={log.flags} />
        </TableCell>
        <TableCell
          sx={{
            borderBottom: `1px solid ${colors.border.light}`,
            py: 1,
          }}
        >
          {(log.ipSet || log.hostSet) && (
            <B4Badge color="secondary" label={log.ipSet || log.hostSet} />
          )}
        </TableCell>
        <TableCell
          sx={{
            color: "text.primary",
            borderBottom: `1px solid ${colors.border.light}`,
            cursor: log.domain && !log.hostSet ? "pointer" : "default",
            py: 1,
            "&:hover":
              log.domain && !log.hostSet
                ? {
                    bgcolor: colors.accent.primary,
                    color: colors.secondary,
                  }
                : {},
          }}
          onClick={() =>
            log.domain && !log.hostSet && onDomainClick(log.domain)
          }
        >
          <Stack direction="row" spacing={1} alignItems="center">
            {log.tls && (
              <B4Badge variant="outlined" color="primary" label={log.tls} />
            )}
            {log.domain && <Typography>{log.domain}</Typography>}
            <Box sx={{ flex: 1 }} />
            {log.domain && !log.hostSet && (
              <AddIcon
                sx={{
                  fontSize: 16,
                  bgcolor: `${colors.secondary}88`,
                  color: colors.background.default,
                  borderRadius: "50%",
                  cursor: "pointer",
                  "&:hover": {
                    bgcolor: colors.secondary,
                  },
                }}
              />
            )}
          </Stack>
        </TableCell>
        <TableCell
          sx={{
            color: "text.secondary",
            fontFamily: "monospace",
            fontSize: 12,
            borderBottom: `1px solid ${colors.border.light}`,
            py: 1,
          }}
        >
          <Tooltip title={log.source} placement="top" arrow>
            {log.deviceName ? (
              <B4Badge label={log.deviceName} color="primary" />
            ) : (
              <span>{log.source}</span>
            )}
          </Tooltip>
        </TableCell>
        <TableCell
          sx={{
            color: "text.primary",
            borderBottom: `1px solid ${colors.border.light}`,
            py: 1,
          }}
        >
          <Stack direction="row" spacing={1} alignItems="center">
            <Box
              sx={{
                cursor: log.ipSet ? "default" : "pointer",
                "&:hover": log.ipSet
                  ? {}
                  : {
                      bgcolor: colors.accent.primary,
                      color: colors.secondary,
                    },
              }}
              onClick={() =>
                log.destination && !log.ipSet && onIpClick(log.destination)
              }
            >
              {log.destination}
            </Box>
            {asnInfo ? (
              <B4Badge
                variant="outlined"
                color="primary"
                label={asnInfo.name}
                onDelete={() => onDeleteAsn(asnInfo.id)}
              />
            ) : (
              log.destination && (
                <Tooltip
                  title={t("connections.table.enrichAsn")}
                  placement="top"
                  arrow
                >
                  {isEnriching ? (
                    <CircularProgress
                      size={14}
                      sx={{ color: colors.secondary }}
                    />
                  ) : (
                    <NetworkIcon
                      onClick={(e) => {
                        e.stopPropagation();
                        void onEnrichIp(log.destination);
                      }}
                      sx={{
                        fontSize: 16,
                        color: `${colors.secondary}88`,
                        cursor: "pointer",
                        "&:hover": {
                          color: colors.secondary,
                        },
                      }}
                    />
                  )}
                </Tooltip>
              )
            )}
            <Box sx={{ flex: 1 }} />
            {!log.ipSet && (
              <AddIcon
                onClick={() => onIpClick(log.destination)}
                sx={{
                  fontSize: 16,
                  bgcolor: `${colors.secondary}88`,
                  color: colors.background.default,
                  borderRadius: "50%",
                  cursor: "pointer",
                  "&:hover": {
                    bgcolor: colors.secondary,
                  },
                }}
              />
            )}
          </Stack>
        </TableCell>
      </TableRow>
    );
  },
  (prev, next) =>
    prev.log.raw === next.log.raw &&
    prev.enrichingIps === next.enrichingIps &&
    prev.asnVersion === next.asnVersion,
);

TableRowMemo.displayName = "TableRowMemo";

export const DomainsTable = ({
  data,
  sortColumn,
  sortDirection,
  onSort,
  onDomainClick,
  onIpClick,
  onEnrichIp,
  onDeleteAsn,
  enrichingIps,
  asnVersion,
  onScrollStateChange,
}: DomainsTableProps) => {
  const { t } = useTranslation();
  const containerRef = useRef<HTMLDivElement>(null);
  const [scrollTop, setScrollTop] = useState(0);
  const [containerHeight, setContainerHeight] = useState(600);

  const startIndex = Math.max(0, Math.floor(scrollTop / ROW_HEIGHT) - OVERSCAN);
  const visibleCount = Math.ceil(containerHeight / ROW_HEIGHT) + OVERSCAN * 2;
  const endIndex = Math.min(data.length, startIndex + visibleCount);

  const visibleData = useMemo(
    () => data.slice(startIndex, endIndex),
    [data, startIndex, endIndex],
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
    <TableContainer
      ref={containerRef}
      onScroll={handleScroll}
      sx={{
        flex: 1,
        backgroundColor: colors.background.dark,
        overflow: "auto",
      }}
    >
      <Table stickyHeader size="small">
        <TableHead>
          <TableRow>
            <SortableTableCell
              label={t("connections.table.time")}
              active={sortColumn === "timestamp"}
              direction={sortColumn === "timestamp" ? sortDirection : null}
              onSort={() => onSort("timestamp")}
            />
            <SortableTableCell
              label={t("connections.table.protocol")}
              active={sortColumn === "protocol"}
              direction={sortColumn === "protocol" ? sortDirection : null}
              onSort={() => onSort("protocol")}
            />
            <SortableTableCell
              label={t("connections.table.set")}
              active={sortColumn === "set"}
              direction={sortColumn === "set" ? sortDirection : null}
              onSort={() => onSort("set")}
            />
            <SortableTableCell
              label={t("connections.table.domain")}
              active={sortColumn === "domain"}
              direction={sortColumn === "domain" ? sortDirection : null}
              onSort={() => onSort("domain")}
            />
            <SortableTableCell
              label={t("connections.table.source")}
              active={sortColumn === "source"}
              direction={sortColumn === "source" ? sortDirection : null}
              onSort={() => onSort("source")}
            />
            <SortableTableCell
              label={t("connections.table.destination")}
              active={sortColumn === "destination"}
              direction={sortColumn === "destination" ? sortDirection : null}
              onSort={() => onSort("destination")}
            />
          </TableRow>
        </TableHead>
        <TableBody>
          {data.length === 0 ? (
            <TableRow>
              <TableCell
                colSpan={6}
                sx={{
                  textAlign: "center",
                  py: 4,
                  color: "text.secondary",
                  fontStyle: "italic",
                  bgcolor: colors.background.dark,
                  borderBottom: "none",
                }}
              >
                {t("connections.table.waitingForConnections")}
              </TableCell>
            </TableRow>
          ) : (
            <>
              {startIndex > 0 && (
                <TableRow>
                  <TableCell
                    colSpan={6}
                    style={{ height: startIndex * ROW_HEIGHT }}
                    sx={{ p: 0, border: "none" }}
                  />
                </TableRow>
              )}

              {visibleData.map((log) => (
                <TableRowMemo
                  key={log.raw}
                  log={log}
                  onDomainClick={onDomainClick}
                  onIpClick={onIpClick}
                  onEnrichIp={onEnrichIp}
                  onDeleteAsn={onDeleteAsn}
                  enrichingIps={enrichingIps}
                  asnVersion={asnVersion}
                />
              ))}

              {endIndex < data.length && (
                <TableRow>
                  <TableCell
                    colSpan={6}
                    style={{ height: (data.length - endIndex) * ROW_HEIGHT }}
                    sx={{ p: 0, border: "none" }}
                  />
                </TableRow>
              )}
            </>
          )}
        </TableBody>
      </Table>
    </TableContainer>
  );
};
