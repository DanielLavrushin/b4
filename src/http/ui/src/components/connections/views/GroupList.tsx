import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Box, Stack, TableSortLabel, Typography } from "@mui/material";
import { colors } from "@design";
import { SortDirection } from "@common/SortableTableCell";
import { GroupRow, ROW_HEIGHT } from "./GroupRow";
import type { EnrichedGroup } from "@hooks/useConnectionGroups";
import { useTranslation } from "react-i18next";

export const AGG_SORT_COLUMNS = [
  "protocol",
  "domain",
  "destination",
  "set",
  "source",
  "packets",
  "seen",
] as const;

export type AggSortColumn = (typeof AGG_SORT_COLUMNS)[number];

interface Props {
  groups: EnrichedGroup[];
  now: number;
  selectedKey: string | null;
  onSelect: (key: string) => void;
  onAddDomain: (domain: string) => void;
  onAddIp: (ip: string) => void;
  onEnrichAsn: (ip: string) => void;
  enrichingIps: Set<string>;
  sortColumn: AggSortColumn | null;
  sortDirection: SortDirection;
  onSort: (column: AggSortColumn) => void;
}

const OVERSCAN = 6;
const HEADER_HEIGHT = 32;

const SortHeader = ({
  column,
  label,
  sortColumn,
  sortDirection,
  onSort,
  align = "left",
}: {
  column: AggSortColumn;
  label: string;
  sortColumn: AggSortColumn | null;
  sortDirection: SortDirection;
  onSort: (column: AggSortColumn) => void;
  align?: "left" | "right";
}) => {
  const active = sortColumn === column;
  return (
    <Box
      sx={{
        display: "flex",
        alignItems: "center",
        justifyContent: align === "right" ? "flex-end" : "flex-start",
      }}
    >
      <TableSortLabel
        active={active}
        direction={active && sortDirection ? sortDirection : "asc"}
        onClick={() => onSort(column)}
        sx={{
          userSelect: "none",
          color: `${colors.secondary} !important`,
          "&:hover": { color: `${colors.primary} !important` },
          "&.Mui-active": {
            color: `${colors.primary} !important`,
            "& .MuiTableSortLabel-icon": { color: `${colors.primary} !important` },
          },
        }}
      >
        <Typography sx={{ color: "inherit", fontWeight: 600, fontSize: 14 }}>
          {label}
        </Typography>
      </TableSortLabel>
    </Box>
  );
};

export const GroupList = ({
  groups,
  now,
  selectedKey,
  onSelect,
  onAddDomain,
  onAddIp,
  onEnrichAsn,
  enrichingIps,
  sortColumn,
  sortDirection,
  onSort,
}: Props) => {
  const { t } = useTranslation();
  const containerRef = useRef<HTMLDivElement>(null);
  const [scrollTop, setScrollTop] = useState(0);
  const [containerHeight, setContainerHeight] = useState(600);

  const startIndex = Math.max(0, Math.floor(scrollTop / ROW_HEIGHT) - OVERSCAN);
  const visibleCount = Math.ceil(containerHeight / ROW_HEIGHT) + OVERSCAN * 2;
  const endIndex = Math.min(groups.length, startIndex + visibleCount);
  const visible = useMemo(() => groups.slice(startIndex, endIndex), [groups, startIndex, endIndex]);

  const handleScroll = useCallback((e: React.UIEvent<HTMLDivElement>) => {
    setScrollTop(e.currentTarget.scrollTop);
  }, []);

  useEffect(() => {
    const c = containerRef.current;
    if (!c) return;
    const obs = new ResizeObserver((entries) => {
      for (const e of entries) setContainerHeight(e.contentRect.height);
    });
    obs.observe(c);
    setContainerHeight(c.clientHeight);
    return () => obs.disconnect();
  }, []);

  return (
    <Box sx={{ flex: 1, display: "flex", flexDirection: "column", overflow: "hidden" }}>
      <Box
        sx={{
          height: HEADER_HEIGHT,
          display: "flex",
          alignItems: "center",
          gap: 1.5,
          px: 2,
          borderBottom: `2px solid ${colors.border.default}`,
          bgcolor: colors.background.paper,
          color: colors.secondary,
          fontWeight: 600,
          position: "sticky",
          top: 0,
          zIndex: 1,
        }}
      >
        <Box sx={{ width: 170, flexShrink: 0 }}>
          <SortHeader
            column="protocol"
            label={t("connections.table.protocol")}
            sortColumn={sortColumn}
            sortDirection={sortDirection}
            onSort={onSort}
          />
        </Box>
        <Box sx={{ flex: 2, minWidth: 0 }}>
          <SortHeader
            column="domain"
            label={t("connections.table.domain")}
            sortColumn={sortColumn}
            sortDirection={sortDirection}
            onSort={onSort}
          />
        </Box>
        <Box sx={{ flex: 1.5, minWidth: 0 }}>
          <SortHeader
            column="destination"
            label={t("connections.table.destination")}
            sortColumn={sortColumn}
            sortDirection={sortDirection}
            onSort={onSort}
          />
        </Box>
        <Box sx={{ width: 130, flexShrink: 0 }}>
          <SortHeader
            column="set"
            label={t("connections.table.set")}
            sortColumn={sortColumn}
            sortDirection={sortDirection}
            onSort={onSort}
          />
        </Box>
        <Box sx={{ width: 100, flexShrink: 0 }}>
          <SortHeader
            column="source"
            label={t("connections.table.source")}
            sortColumn={sortColumn}
            sortDirection={sortDirection}
            onSort={onSort}
          />
        </Box>
        <Box sx={{ width: 120, flexShrink: 0 }}>
          <Typography sx={{ color: colors.secondary, fontWeight: 600, fontSize: 14 }}>
            {t("connections.aggregated.activity")}
          </Typography>
        </Box>
        <Box sx={{ width: 60, flexShrink: 0 }}>
          <SortHeader
            column="packets"
            label={t("connections.aggregated.packets")}
            sortColumn={sortColumn}
            sortDirection={sortDirection}
            onSort={onSort}
            align="right"
          />
        </Box>
        <Box sx={{ width: 48, flexShrink: 0 }}>
          <SortHeader
            column="seen"
            label={t("connections.aggregated.seen")}
            sortColumn={sortColumn}
            sortDirection={sortDirection}
            onSort={onSort}
            align="right"
          />
        </Box>
      </Box>

      <Box
        ref={containerRef}
        onScroll={handleScroll}
        sx={{ flex: 1, overflow: "auto", bgcolor: colors.background.dark }}
      >
        {groups.length === 0 ? (
          <Stack sx={{ py: 6, alignItems: "center", color: colors.text.disabled }}>
            <Typography sx={{ fontStyle: "italic" }}>
              {t("connections.table.waitingForConnections")}
            </Typography>
          </Stack>
        ) : (
          <>
            <Box sx={{ height: startIndex * ROW_HEIGHT }} />
            {visible.map((g) => (
              <GroupRow
                key={g.key}
                group={g}
                now={now}
                selected={selectedKey === g.key}
                onSelect={onSelect}
                onAddDomain={onAddDomain}
                onAddIp={onAddIp}
                onEnrichAsn={onEnrichAsn}
                enrichingIps={enrichingIps}
              />
            ))}
            <Box sx={{ height: Math.max(0, (groups.length - endIndex) * ROW_HEIGHT) }} />
          </>
        )}
      </Box>
    </Box>
  );
};
