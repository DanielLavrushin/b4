import { useState, useEffect, useCallback, useMemo } from "react";
import { Box, Fab, Tooltip } from "@mui/material";
import { StartIcon, StopIcon } from "@b4.icons";
import { colors } from "@design";
import { DomainsControlBar } from "../ControlBar";
import { DomainsTable, SortColumn } from "../Table";
import { SortDirection } from "@common/SortableTableCell";
import {
  ParsedLog,
  useEnrichedLogs,
  useFilteredLogs,
  useSortedLogs,
} from "@hooks/useDomainActions";
import { AsnLabels, loadSortState, saveSortState } from "@utils";
import { useTranslation } from "react-i18next";

const MAX_DISPLAY_ROWS = 1000;

interface Props {
  entries: ParsedLog[];
  deviceMap: Record<string, string>;
  paused: boolean;
  onTogglePause: () => void;
  showAll: boolean;
  onShowAllChange: (v: boolean) => void;
  onReset: () => void;
  filter: string;
  onFilterChange: (v: string) => void;
  enrichingIps: Set<string>;
  asnLabels: AsnLabels;
  onAddDomain: (domain: string) => void;
  onAddIp: (ip: string) => void;
  onEnrichIp: (ip: string) => Promise<void>;
  onDeleteAsn: (asnId: string) => void;
}

export const RawView = ({
  entries,
  deviceMap,
  paused,
  onTogglePause,
  showAll,
  onShowAllChange,
  onReset,
  filter,
  onFilterChange,
  enrichingIps,
  asnLabels,
  onAddDomain,
  onAddIp,
  onEnrichIp,
  onDeleteAsn,
}: Props) => {
  const { t } = useTranslation();

  const [sortColumn, setSortColumn] = useState<SortColumn | null>(() => {
    const saved = loadSortState();
    return saved.column as SortColumn | null;
  });
  const [sortDirection, setSortDirection] = useState<SortDirection>(() => {
    const saved = loadSortState();
    return saved.direction;
  });

  useEffect(() => {
    saveSortState(sortColumn, sortDirection);
  }, [sortColumn, sortDirection]);

  const parsedLogs = useMemo(() => {
    const recent = entries.length > MAX_DISPLAY_ROWS ? entries.slice(-MAX_DISPLAY_ROWS) : entries;
    return showAll ? recent : recent.filter((log) => log.domain !== "");
  }, [entries, showAll]);

  const enrichedLogs = useEnrichedLogs(parsedLogs, deviceMap);
  const filteredLogs = useFilteredLogs(enrichedLogs, filter, asnLabels);
  const sortedData = useSortedLogs(filteredLogs, sortColumn, sortDirection);

  const handleSort = useCallback((column: SortColumn) => {
    setSortColumn((prevColumn) => {
      if (prevColumn === column) {
        setSortDirection((prevDir) => {
          if (prevDir === "asc") return "desc";
          if (prevDir === "desc") {
            setSortColumn(null);
            return null;
          }
          return "asc";
        });
        return prevColumn;
      }
      setSortDirection("asc");
      return column;
    });
  }, []);

  const handleClearSort = useCallback(() => {
    setSortColumn(null);
    setSortDirection(null);
  }, []);

  const handleScrollStateChange = useCallback(() => {}, []);

  return (
    <>
      <DomainsControlBar
        filter={filter}
        onFilterChange={onFilterChange}
        filteredCount={filteredLogs.length}
        sortColumn={sortColumn}
        showAll={showAll}
        onShowAllChange={onShowAllChange}
        onClearSort={handleClearSort}
        onReset={onReset}
      />
      <Box
        sx={{
          position: "relative",
          flex: 1,
          overflow: "hidden",
          display: "flex",
        }}
      >
        <DomainsTable
          data={sortedData}
          sortColumn={sortColumn}
          sortDirection={sortDirection}
          onSort={handleSort}
          onDomainClick={onAddDomain}
          onIpClick={onAddIp}
          onEnrichIp={onEnrichIp}
          onDeleteAsn={onDeleteAsn}
          enrichingIps={enrichingIps}
          asnLabels={asnLabels}
          onScrollStateChange={handleScrollStateChange}
        />
        <Tooltip
          title={
            paused
              ? t("connections.page.resumeStreaming")
              : t("connections.page.pauseStreaming")
          }
          placement="left"
        >
          <Fab
            size="small"
            onClick={onTogglePause}
            sx={{
              position: "absolute",
              bottom: 16,
              right: 16,
              bgcolor: paused ? colors.secondary : colors.border.strong,
              color: colors.background.default,
              "&:hover": {
                bgcolor: paused ? colors.secondary : colors.border.default,
              },
            }}
          >
            {paused ? <StartIcon /> : <StopIcon />}
          </Fab>
        </Tooltip>
      </Box>
    </>
  );
};
